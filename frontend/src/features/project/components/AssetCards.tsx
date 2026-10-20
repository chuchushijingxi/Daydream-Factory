import { protectedResourceUrl } from '../../../api/client';
import { useRef, useState, type PointerEvent } from 'react';
import { ArrowUpRight, LoaderCircle, MoreHorizontal, Plus, Trash2, Upload } from 'lucide-react';
import type { Asset } from '../../../api/types';
import { Button } from '../../../components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../../components/ui/dropdown-menu';
import { useCatalogText } from '../../../contexts/useCatalogText';
import { MediaGenerator } from '../../media/MediaGenerator';
import { useAssetImageUpload } from './useAssetImageUpload';
import { AssetImageTaskStatus } from '../../media/AssetImageTasks';

const assetLabels = { character: '角色', location: '场景', prop: '道具' };
const addLabels = { character: '新增角色', location: '新增场景', prop: '新增道具' };
export function AssetCard({
  asset,
  editable,
  onClick,
  onDelete,
}: {
  asset: Asset;
  editable: boolean;
  onClick: () => void;
  onDelete: () => void;
}) {
  const { c } = useCatalogText();
  const item = useRef<HTMLElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const more = useRef<HTMLButtonElement>(null);
  const deleting = useRef(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const [pointerMode, setPointerMode] = useState(false);
  const { busy, error, pending, readback, enabled, upload, recover } = useAssetImageUpload({
    target: { assetId: asset.id },
    editable,
  });
  const [failedThumbnail, setFailedThumbnail] = useState<string>();
  const thumbnail = asset.image?.thumbnailUrl;
  const imageFailed = Boolean(thumbnail && failedThumbnail === thumbnail);
  const description = asset.description || asset.character?.biography;
  const leaveTools = (event: PointerEvent<HTMLElement>) => {
    if (event.pointerType === 'touch') return;
    setPointerMode(true);
    const next = event.relatedTarget;
    if (
      next instanceof Node &&
      (item.current?.contains(next) ||
        (next instanceof Element &&
          next.closest('.asset-catalog-menu')?.getAttribute('data-asset-menu') === asset.id))
    )
      return;
    setMenuOpen(false);
  };

  return (
    <article
      ref={item}
      className="asset-catalog-item"
      data-menu-open={menuOpen || undefined}
      data-pointer-mode={pointerMode || undefined}
      aria-busy={busy}
      onPointerLeave={leaveTools}
      onPointerDownCapture={() => setPointerMode(true)}
      onKeyDownCapture={() => setPointerMode(false)}
    >
      <div className="asset-catalog-card-frame">
        <button
          type="button"
          className="asset-catalog-card"
          aria-label={c('查看 {name}', { name: asset.name })}
          disabled={busy}
          onClick={onClick}
        >
          <span className="asset-catalog-media">
            {thumbnail && !imageFailed ? (
              <img
                src={protectedResourceUrl(thumbnail)}
                alt={asset.name}
                loading="lazy"
                onError={() => setFailedThumbnail(thumbnail)}
              />
            ) : (
              <span className="asset-catalog-placeholder">
                {c(imageFailed ? '缩略图读取失败' : '暂无预览')}
              </span>
            )}
            <AssetImageTaskStatus target={{ assetId: asset.id }} image={asset.image} />
          </span>
          <span className="asset-catalog-copy">
            <span className="asset-catalog-identity">
              <span className="asset-catalog-name">{asset.name}</span>
              <span className="asset-catalog-meta">
                {c(assetLabels[asset.kind])}
                {asset.aliases.length > 0 ? <> · {asset.aliases.join(' / ')}</> : null}
              </span>
            </span>
            <span className="asset-catalog-description" data-empty={!description || undefined}>
              {description || c('暂无描述')}
            </span>
            <span className="asset-catalog-card-footer">
              <ArrowUpRight className="asset-catalog-arrow" aria-hidden />
            </span>
          </span>
        </button>
        <div className="asset-catalog-tool-layer">
          <div
            className="asset-catalog-tools"
            role="group"
            aria-label={c('{name} · {label}', { name: asset.name, label: c('更多') })}
          >
            <input
              ref={input}
              type="file"
              accept="image/*"
              hidden
              aria-label={c('上传{name}', { name: asset.name })}
              disabled={!enabled || busy || readback}
              onChange={(event) => {
                const file = event.currentTarget.files?.[0];
                event.currentTarget.value = '';
                if (file) void upload(file);
              }}
            />
            <button
              type="button"
              className="asset-catalog-tool"
              aria-label={c('上传{name}', { name: asset.name })}
              title={c(busy ? '上传并保存中…' : '上传')}
              disabled={!enabled || busy || readback}
              onClick={() => input.current?.click()}
            >
              {busy ? (
                <LoaderCircle className="animate-spin" aria-hidden />
              ) : (
                <Upload aria-hidden />
              )}
            </button>
            <MediaGenerator
              kind="image"
              target={{ assetId: asset.id }}
              renderEntry={(open, entry) => (
                <button
                  type="button"
                  className="asset-catalog-tool"
                  aria-label={c('{label}：{name}', { label: c(entry.label), name: asset.name })}
                  disabled={busy}
                  onClick={open}
                >
                  <span className="asset-catalog-ai-icon" aria-hidden>
                    AI
                  </span>
                </button>
              )}
            />
            <DropdownMenu modal={false} open={menuOpen} onOpenChange={setMenuOpen}>
              <DropdownMenuTrigger asChild>
                <button
                  ref={more}
                  type="button"
                  className="asset-catalog-tool"
                  aria-label={c('{label}：{name}', { label: c('更多'), name: asset.name })}
                  title={c('更多')}
                  disabled={busy}
                >
                  <MoreHorizontal aria-hidden />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent
                align="end"
                className="asset-catalog-menu"
                data-asset-menu={asset.id}
                onPointerLeave={leaveTools}
                onCloseAutoFocus={(event) => {
                  if (deleting.current) {
                    event.preventDefault();
                    deleting.current = false;
                    if (more.current?.isConnected) {
                      more.current.focus({ preventScroll: true });
                      onDelete();
                    }
                  }
                }}
              >
                <DropdownMenuItem
                  variant="destructive"
                  disabled={!editable || busy}
                  onSelect={() => {
                    deleting.current = true;
                  }}
                >
                  <Trash2 aria-hidden />
                  {c('删除')}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </div>
      {error ? (
        <div role="alert" className="jm-inline-error asset-catalog-error">
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
    </article>
  );
}

export function AddAssetCard({
  kind,
  disabled,
  onClick,
}: {
  kind: Asset['kind'];
  disabled: boolean;
  onClick: () => void;
}) {
  const { c } = useCatalogText();
  return (
    <button type="button" className="asset-catalog-add" disabled={disabled} onClick={onClick}>
      <span className="asset-catalog-add-icon">
        <Plus aria-hidden strokeWidth={1.5} />
      </span>
      <span>{c(addLabels[kind])}</span>
    </button>
  );
}
