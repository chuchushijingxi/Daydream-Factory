import { UploadPicker } from '../../../components/spaces/UploadPicker';
import { protectedResourceUrl } from '../../../api/client';
import { useEffect, useRef, useState } from 'react';
import { Download, Upload, LoaderCircle, Eye } from 'lucide-react';
import type { AssetImage, Project, VisualTarget } from '../../../api/types';
import { Button } from '../../../components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '../../../components/ui/dialog';
import { useCatalogText } from '../../../contexts/useCatalogText';
import { ImageUnavailablePlaceholder } from './ImageUnavailablePlaceholder';
import { useAssetImageUpload } from './useAssetImageUpload';
import { ImageFinalPicker } from '../../media/ImageFinalPicker';
import { MediaGenerator } from '../../media/MediaGenerator';
import { workflow } from '../../../domain/project-workflow';
import { useWorkspace } from '../../../state/WorkspaceContext';
import { AssetImageTaskStatus } from '../../media/AssetImageTasks';

export function AssetImageCard({
  label,
  image,
  target,
  editable,
  active = true,
  showGenerate = true,
  onSaved,
  onBusy,
}: {
  label: string;
  image?: AssetImage | null;
  target?: VisualTarget;
  editable: boolean;
  active?: boolean;
  showGenerate?: boolean;
  onSaved?: (project: Project, expectedRevision: number) => void;
  onBusy?: (busy: boolean) => void;
}) {
  const { c } = useCatalogText();
  const { currentProject: p } = useWorkspace();
  const input = useRef<HTMLInputElement>(null);
  const { scope, busy, error, pending, readback, enabled, upload, select, recover } =
    useAssetImageUpload({
      target,
      editable,
      active,
      onSaved,
    });
  const [preview, setPreview] = useState(false);
  const [selecting, setSelecting] = useState(false);
  const [thumbnailFailed, setThumbnailFailed] = useState(false);
  const [originalFailed, setOriginalFailed] = useState(false);
  const [retryImage, setRetryImage] = useState(0);
  useEffect(() => {
    setPreview(false);
  }, [scope, active]);
  useEffect(() => {
    onBusy?.(busy);
    return () => onBusy?.(false);
  }, [busy, onBusy]);
  useEffect(() => {
    setThumbnailFailed(false);
    setOriginalFailed(false);
  }, [image?.fileId, retryImage]);
  return (
    <figure className="asset-image-card" aria-label={label} aria-busy={busy}>
      <div className="asset-image-preview">
        <AssetImageTaskStatus target={target} image={image} />
        {image ? (
          <button
            type="button"
            className="asset-image-open"
            aria-label={c('查看原图：{name}', { name: label })}
            onClick={(event) => {
              event.currentTarget.focus({ preventScroll: true });
              setOriginalFailed(false);
              setPreview(true);
            }}
          >
            {thumbnailFailed ? (
              <span className="asset-image-message">{c('缩略图读取失败')}</span>
            ) : (
              <img
                key={image.fileId + retryImage}
                src={protectedResourceUrl(image.thumbnailUrl)}
                alt={label}
                onError={() => setThumbnailFailed(true)}
              />
            )}
          </button>
        ) : (
          <ImageUnavailablePlaceholder />
        )}
        {image ? (
          <div className="asset-image-overlay asset-image-overlay-preview">
            <button
              type="button"
              className="asset-image-chip"
              aria-label={c('查看原图：{name}', { name: label })}
              onClick={(event) => {
                event.currentTarget.focus({ preventScroll: true });
                setOriginalFailed(false);
                setPreview(true);
              }}
            >
              <Eye aria-hidden />
              {c('预览')}
            </button>
          </div>
        ) : null}
        <div className="asset-image-overlay asset-image-overlay-actions">
          {thumbnailFailed ? (
            <button
              type="button"
              className="asset-image-chip"
              onClick={() => setRetryImage((v) => v + 1)}
            >
              {c('重试预览')}
            </button>
          ) : null}
          <input
            ref={input}
            type="file"
            accept="image/*"
            hidden
            aria-label={c('上传{name}', { name: label })}
            disabled={!enabled || busy || readback}
            onChange={(event) => {
              const file = event.currentTarget.files?.[0];
              event.currentTarget.value = '';
              if (file) void upload(file);
            }}
          />
          <button
            type="button"
            className="asset-image-chip"
            disabled={!enabled || busy || readback}
            onClick={() => input.current?.click()}
          >
            {busy ? <LoaderCircle className="animate-spin" aria-hidden /> : <Upload aria-hidden />}
            {busy ? c('上传并保存中…') : c('上传')}
          </button>
          <button
            type="button"
            className="asset-image-chip"
            disabled={!enabled || busy || readback}
            onClick={() => setSelecting(true)}
          >
            {c('选择已上传')}
          </button>
          {image ? (
            <a className="asset-image-chip" href={protectedResourceUrl(image.originalUrl)} download>
              <Download aria-hidden />
              {c('下载')}
            </a>
          ) : null}
        </div>
      </div>
      {target && showGenerate ? (
        <MediaGenerator
          target={target}
          onAdopt={onSaved}
          kind="image"
          disabled={!p || !workflow.effectivePrompt(p, target)}
        />
      ) : null}
      {target ? (
        <ImageFinalPicker
          key={p?.id + JSON.stringify(target)}
          target={target}
          image={image}
          onSaved={onSaved}
          onBusy={onBusy}
        />
      ) : null}
      {!target ? <p className="asset-image-message">{c('先保存资料，再上传图片。')}</p> : null}
      {error ? (
        <div role="alert" className="jm-inline-error asset-image-message">
          <p>{error}</p>
          {pending ? (
            <Button
              variant="link"
              size="sm"
              disabled={busy || (!readback && !enabled)}
              onClick={() => void recover()}
            >
              {readback ? c('读取最新版本') : c('重试关联')}
            </Button>
          ) : null}
        </div>
      ) : null}
      <UploadPicker open={selecting} onOpenChange={setSelecting} onSelect={select} />
      <Dialog open={preview && Boolean(image)} onOpenChange={setPreview}>
        <DialogContent
          variant="preview"
          className="asset-original-dialog"
          overlayClassName="asset-original-mask"
        >
          <DialogTitle className="sr-only">{c('查看原图：{name}', { name: label })}</DialogTitle>
          <DialogDescription className="sr-only">{c('原始上传图片')}</DialogDescription>
          {originalFailed ? (
            <div role="alert" className="asset-original-error">
              <p>{c('原图读取失败')}</p>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  setOriginalFailed(false);
                  setRetryImage((v) => v + 1);
                }}
              >
                {c('重试预览')}
              </Button>
            </div>
          ) : image ? (
            <img
              key={image.fileId + retryImage}
              src={protectedResourceUrl(image.originalUrl)}
              alt={label}
              onError={() => setOriginalFailed(true)}
            />
          ) : null}
        </DialogContent>
      </Dialog>
    </figure>
  );
}
