import { Fragment, useEffect, useId, useState } from 'react';
import { createPortal } from 'react-dom';
import { CircleAlert, Clock3, LoaderCircle, Plus, Trash2 } from 'lucide-react';
import type { StoryShot } from '../../api/storyboard-types';
import { Button } from '../../components/ui/button';
import { protectedResourceUrl } from '../../api/client';
import { useShotDrag } from './useShotDrag';
import { useCatalogText } from '../../contexts/useCatalogText';

function taskLabel(shot: StoryShot, t: (text: string) => string) {
  const status = shot.videoTask?.status;
  if (status === 'queued') return t('排队中');
  if (status === 'running') return t('生成中');
  if (status === 'unknown') return t('待确认');
  if (status === 'failed') return t('生成失败');
  return '';
}

function ShotPreview({ shot, number }: { shot: StoryShot; number: number }) {
  const { c } = useCatalogText();
  const status = shot.videoTask?.status, label = taskLabel(shot, c);
  const seconds = shot.previewDurationSeconds;
  const duration = seconds && seconds > 0
    ? `${String(Math.floor(seconds / 60)).padStart(2, '0')}:${String(Math.floor(seconds % 60)).padStart(2, '0')}` : '';
  return <>
    {shot.thumbnailUrl ? <img className="sb-strip-cover" src={protectedResourceUrl(shot.thumbnailUrl)} alt="" loading="lazy" draggable={false} />
      : !label ? <span className="sb-strip-pending">{c('待生成')}</span> : null}
    {label ? <span className="sb-strip-task" data-status={status} data-covered={!!shot.thumbnailUrl} role="status">
      {status === 'queued' || status === 'running' ? <LoaderCircle className="animate-spin" aria-hidden="true" />
        : status === 'unknown' ? <Clock3 aria-hidden="true" /> : <CircleAlert aria-hidden="true" />}
      <span>{label}</span>
    </span> : null}
    {shot.thumbnailUrl && duration ? <span className="sb-strip-duration">{duration}</span> : null}
    <span className="sb-strip-number">{number}</span>
  </>;
}

export function ShotStrip({
  shots,
  currentId,
  revision,
  disabled,
  onSelect,
  onReorder,
  onInsert,
  onDelete,
  emptyMessage,
}: {
  shots: StoryShot[];
  currentId?: string;
  revision: number;
  disabled: boolean;
  onSelect: (id: string) => void;
  onReorder: (ids: string[], revision: number) => Promise<boolean>;
  onInsert: (afterId?: string) => void;
  onDelete: (shot: StoryShot) => void;
  emptyMessage?: string;
}) {
  const { c } = useCatalogText();
  const [hovered, setHovered] = useState('');
  const drag = useShotDrag({ shots, revision, disabled, onReorder });
  const help = useId();
  useEffect(() => {
    setHovered('');
  }, [revision, disabled, drag.floating?.shot.id]);
  return (
    <section className="sb-filmstrip" aria-label={c('本场景分镜')} data-drag-active={!!drag.floating}
      onPointerDownCapture={drag.resetClick} onKeyDownCapture={drag.resetClick}
      onClickCapture={(event) => { if (drag.consumeClick()) { event.preventDefault(); event.stopPropagation(); } }}>
      <span id={help} className="sr-only">
        {c('拖动镜头调整顺序，也可按住 Alt 和左右方向键移动。')}
      </span>
      <div className="sb-strip-track" ref={drag.track}>
        {!shots.length ? (
          <p className="sb-strip-empty">{c(emptyMessage ?? '暂无分镜，点击新增开始。')}</p>
        ) : null}
        <div className="sb-strip-items">
          {shots.map((shot, index) => {
            const status = shot.videoTask?.status;
            const generating = status === 'queued' || status === 'running';
            const label = taskLabel(shot, c), number = drag.number(shot.id, index);
            return (
              <Fragment key={shot.id}>
                <div
                  className="sb-strip-card"
                  ref={(node) => { if (node) drag.cards.current.set(shot.id, node); else drag.cards.current.delete(shot.id); }}
                  style={drag.style(shot.id)}
                  data-current={currentId === shot.id}
                  data-hovered={hovered === shot.id}
                  data-dragging={drag.floating?.shot.id === shot.id}
                  onPointerEnter={() => {
                    if (!drag.floating) setHovered(shot.id);
                  }}
                  onPointerMove={() => {
                    if (!drag.floating) setHovered(shot.id);
                  }}
                  onPointerLeave={() => setHovered((current) => (current === shot.id ? '' : current))}
                >
                  <button
                    type="button"
                    className="sb-strip-select"
                    aria-label={
                      shot.action && label
                        ? c('镜头 {number}：{action}，{label}', {
                            number,
                            action: shot.action,
                            label,
                          })
                        : shot.action
                          ? c('镜头 {number}：{action}', { number, action: shot.action })
                          : label
                            ? c('镜头 {number}，{label}', { number, label })
                            : c('镜头 {number}', { number })
                    }
                    aria-busy={generating || undefined}
                    aria-current={currentId === shot.id ? 'true' : undefined}
                    aria-describedby={help}
                    aria-keyshortcuts="Alt+ArrowLeft Alt+ArrowRight"
                    title={shot.action || c('分镜 {number}', { number: index + 1 })}
                    draggable={false}
                    onPointerDown={(event) => drag.pointerDown(event, shot, index)}
                    onClick={() => onSelect(shot.id)}
                    onKeyDown={(event) => {
                      if (!event.altKey || !['ArrowLeft', 'ArrowRight'].includes(event.key)) return;
                      event.preventDefault();
                      const next = shots[index + (event.key === 'ArrowLeft' ? -1 : 1)];
                      if (next) drag.move(shot.id, next.id);
                    }}
                  >
                    <ShotPreview shot={shot} number={number} />
                  </button>
                  <button
                    type="button"
                    className="sb-strip-delete"
                    aria-label={c('删除镜头 {number}', { number: index + 1 })}
                    disabled={disabled}
                    draggable={false}
                    onDragStart={(event) => event.stopPropagation()}
                    onClick={() => onDelete(shot)}
                  >
                    <Trash2 />
                  </button>
                </div>
                {index < shots.length - 1 ? (
                  <div className="sb-strip-gap">
                    <div className="sb-strip-gap-target">
                      <button
                        type="button"
                        aria-label={c('在镜头 {number} 后插入镜头', { number: index + 1 })}
                        disabled={disabled}
                        onClick={() => onInsert(shot.id)}
                      >
                        <Plus aria-hidden="true" />
                      </button>
                    </div>
                  </div>
                ) : null}
              </Fragment>
            );
          })}
        </div>
        {shots.length ? <div className="sb-strip-gap sb-strip-gap--tail" aria-hidden="true" /> : null}
        <Button
          className="sb-strip-add"
          aria-label={c('新增')}
          variant="ghost"
          size="sm"
          disabled={disabled}
          onClick={() => onInsert(shots.at(-1)?.id)}
        >
          <svg className="sb-strip-add-border" aria-hidden="true">
            <rect
              x=".5" y=".5" width="calc(100% - 1px)" height="calc(100% - 1px)"
              rx="11" ry="11" fill="none" stroke="var(--credit-accent)" strokeOpacity=".6" strokeWidth="1"
              strokeDasharray="3 3" vectorEffect="non-scaling-stroke"
            />
          </svg>
          <Plus aria-hidden="true" />
          <span>{c('新增')}</span>
        </Button>
      </div>
      {drag.floating ? createPortal(
        <div className="sb-strip-floating" aria-hidden="true" data-settling={drag.floating.settling}
          style={{ left: drag.floating.x, top: drag.floating.y, width: drag.floating.width, height: drag.floating.height,
            transform: `rotate(${drag.floating.angle}deg) scale(${drag.floating.settling ? 1 : 1.03})` }}>
          <ShotPreview shot={drag.floating.shot} number={drag.floating.number} />
        </div>, document.body,
      ) : null}
    </section>
  );
}
