import { useId, useState } from 'react';
import { Download } from 'lucide-react';
import type { Project, VisualTarget } from '../../api/types';
import type { EpisodeSourceView } from '../../api/storyboard-types';
import { protectedResourceUrl } from '../../api/client';
import { assetLabel } from '../../domain/storyboard';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '../../components/ui/dialog';
import { ViewTabs } from '../../components/ui/view-tabs';
import { ImageWithFallback } from '../../components/media/ImageWithFallback';
import { useCatalogText } from '../../contexts/useCatalogText';

export function ReferenceDialog({
  open,
  onOpenChange,
  project,
  selected,
  currentSource,
  source,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  project: Project;
  selected: VisualTarget[];
  currentSource: string[];
  source: EpisodeSourceView | null;
}) {
  const { c } = useCatalogText();
  const [tab, setTab] = useState('assets'),
    [preview, setPreview] = useState<{ url: string; name: string } | null>(null),
    id = useId();
  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent variant="panel" className="sb-reference-dialog">
          <DialogTitle>{c('参考资料')}</DialogTitle>
          <DialogDescription className="sr-only">{c('查看当前镜头的资产与剧本原文。')}</DialogDescription>
          <ViewTabs
            label={c('参考资料分类')}
            prefix={id}
            value={tab}
            onChange={setTab}
            panelId={id + '-content'}
            items={[
              { value: 'assets', label: c('资产') },
              { value: 'source', label: c('原文') },
            ]}
          />
          <div
            className="sb-reference-content"
            role="tabpanel"
            id={id + '-content'}
            aria-labelledby={id + tab}
          >
            {tab === 'assets' ? (
              <div className="sb-reference-assets">
                {selected.map((ref) => {
                  const asset = project.assets.find((a) => a.id === ref.assetId),
                    look = asset?.looks.find((l) => l.id === ref.lookId),
                    image = ref.lookId ? look?.image : asset?.image,
                    name = assetLabel(project.assets, ref, c);
                  const prompt = project.visualPrompts.find(
                    (v) =>
                      v.target.assetId === ref.assetId &&
                      (v.target.lookId || '') === (ref.lookId || ''),
                  )?.text;
                  const description = look?.description || asset?.description || prompt;
                  return (
                    <article className="sb-reference-asset" key={JSON.stringify(ref)}>
                      <button
                        className="sb-reference-thumb"
                        type="button"
                        disabled={!image}
                        aria-label={c('预览图片：{name}', { name })}
                        onClick={() => {
                          if (image) setPreview({ url: image.originalUrl, name });
                        }}
                      >
                        {image ? (
                          <ImageWithFallback src={image.thumbnailUrl} alt={name} />
                        ) : (
                          <span>{c('暂无图片')}</span>
                        )}
                      </button>
                      <div>
                        <h3>{name}</h3>
                        <p>{description || (asset ? c('暂无描述') : c('对象已不可用'))}</p>
                        {(asset?.character && Object.values(asset.character).some(Boolean)) ||
                        (prompt && description !== prompt) ? (
                          <details>
                            <summary>{c('详细资料')}</summary>
                            {asset?.character
                              ? Object.entries(asset.character).map(([key, value]) => (
                                  <p key={key}>{value}</p>
                                ))
                              : null}
                            {prompt && description !== prompt ? <p>{prompt}</p> : null}
                          </details>
                        ) : null}
                        {image ? (
                          <a
                            className="sb-reference-download"
                            href={protectedResourceUrl(image.originalUrl)}
                            download
                          >
                            <Download />
                            {c('下载原图')}
                          </a>
                        ) : null}
                      </div>
                    </article>
                  );
                })}
                {!selected.length ? <p className="sb-reference-empty">{c('暂无参考素材')}</p> : null}
              </div>
            ) : (
              <div className="sb-reference-source">
                <h3>{c('当前镜头')}</h3>
                {currentSource.length ? (
                  currentSource.map((text, i) => <p key={i}>{text}</p>)
                ) : (
                  <p className="sb-muted">{c('暂无镜头原文')}</p>
                )}
                <details>
                  <summary>{c('本集完整原文')}</summary>
                  {source?.fragments.map((fragment, i) => <p key={i}>{fragment.text}</p>)}
                </details>
              </div>
            )}
          </div>
        </DialogContent>
      </Dialog>
      <Dialog
        open={!!preview}
        onOpenChange={(value) => {
          if (!value) setPreview(null);
        }}
      >
        <DialogContent variant="preview" className="asset-original-dialog">
          <DialogTitle className="sr-only">{preview?.name}</DialogTitle>
          <DialogDescription className="sr-only">{c('资产原图')}</DialogDescription>
          {preview ? <ImageWithFallback src={preview.url} alt={preview.name} /> : null}
        </DialogContent>
      </Dialog>
    </>
  );
}
