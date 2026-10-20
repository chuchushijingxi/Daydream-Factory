import { useEffect, useId, useState } from 'react';
import { Search } from 'lucide-react';
import type { Asset, VisualTarget } from '../../api/types';
import { assetLabel, refKey, sameRef } from '../../domain/storyboard';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '../../components/ui/dialog';
import { ViewTabs } from '../../components/ui/view-tabs';
import { ImageWithFallback } from '../../components/media/ImageWithFallback';
import { useCatalogText } from '../../contexts/useCatalogText';

export function AssetPicker({
  open,
  onClose,
  assets,
  selected,
  onConfirm,
  disabled,
}: {
  open: boolean;
  onClose: () => void;
  assets: Asset[];
  selected: VisualTarget[];
  onConfirm: (refs: VisualTarget[]) => void;
  disabled: boolean;
}) {
  const { c } = useCatalogText();
  const [draft, setDraft] = useState(selected),
    [kind, setKind] = useState<Asset['kind']>(
      () =>
        assets.find((a) => a.id === selected[0]?.assetId)?.kind || assets[0]?.kind || 'character',
    ),
    [query, setQuery] = useState('');
  const id = useId();
  useEffect(() => {
    if (open) {
      setDraft(selected);
      setQuery('');
    }
  }, [open]);
  const visible = assets
    .filter((a) => a.kind === kind)
    .flatMap((a) =>
      [
        { assetId: a.id },
        ...(a.kind === 'character' ? a.looks.map((l) => ({ assetId: a.id, lookId: l.id })) : []),
      ].map((ref: VisualTarget) => ({ a, ref })),
    )
    .filter(({ a, ref }) =>
      (
        assetLabel(assets, ref) +
        ' ' +
        a.description +
        ' ' +
        (a.looks.find((l) => l.id === ref.lookId)?.description || '')
      )
        .toLocaleLowerCase()
        .includes(query.toLocaleLowerCase()),
    );
  const toggle = (ref: VisualTarget) =>
    setDraft((old) =>
      old.some((x) => sameRef(x, ref)) ? old.filter((x) => !sameRef(x, ref)) : [...old, ref],
    );
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
    >
      <DialogContent variant="picker" className="sb-asset-picker">
        <DialogTitle>{c('选择参考素材')}</DialogTitle>
        <DialogDescription className="sr-only">{c('选择本镜头使用的资产图片。')}</DialogDescription>
        <div className="sb-picker-toolbar">
          <ViewTabs
            label={c('资产类别')}
            prefix={id}
            variant="pill"
            panelId={id + '-images'}
            value={kind}
            onChange={(value) => setKind(value as Asset['kind'])}
            items={[
              { value: 'character', label: c('角色 / 形象') },
              { value: 'location', label: c('地点') },
              { value: 'prop', label: c('道具') },
            ]}
          />
          <label className="sb-choice-search">
            <Search size={16} />
            <input
              type="search"
              aria-label={c('搜索项目资产')}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={c('搜索资产')}
            />
          </label>
        </div>
        <div
          className="sb-picker-grid"
          role="tabpanel"
          id={id + '-images'}
          aria-labelledby={id + kind}
        >
          {visible.map(({ a, ref }) => {
            const image = ref.lookId ? a.looks.find((l) => l.id === ref.lookId)?.image : a.image,
              checked = draft.some((x) => sameRef(x, ref)),
              name = assetLabel(assets, ref);
            return (
              <article className="sb-picker-card" data-selected={checked} key={refKey(ref)}>
                <label title={name}>
                  <input
                    type="checkbox"
                    aria-label={name}
                    disabled={disabled}
                    checked={checked}
                    onChange={() => toggle(ref)}
                  />
                  <span className="sb-picker-image">
                    {image?.thumbnailUrl ? (
                      <ImageWithFallback src={image.thumbnailUrl} alt={name} />
                    ) : (
                      <span>{c('暂无图片')}</span>
                    )}
                    <span className="sb-picker-check" aria-hidden="true" />
                  </span>
                  <span className="sb-picker-name">{name}</span>
                </label>
              </article>
            );
          })}
          {!visible.length ? (
            <p className="sb-picker-empty">{query ? c('没有匹配的资产') : c('此分类暂无素材')}</p>
          ) : null}
        </div>
        <footer className="sb-picker-footer">
          <span>{c('已选 {count} 项', { count: draft.length })}</span>
          <Button variant="outline" onClick={onClose}>
            {c('取消')}
          </Button>
          <Button
            disabled={disabled}
            onClick={() => {
              onConfirm(draft);
              onClose();
            }}
          >
            {c('确认')}
          </Button>
        </footer>
      </DialogContent>
    </Dialog>
  );
}
