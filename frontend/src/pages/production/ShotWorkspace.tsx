import { useState } from 'react';
import { ImageIcon, Plus } from 'lucide-react';
import type { StoryShot } from '../../api/storyboard-types';
import { assetLabel, referenceLocations, refKey, sameRef } from '../../domain/storyboard';
import { useWorkspace } from '../../state/WorkspaceContext';
import { Button } from '../../components/ui/button';
import { ControlTooltip } from '../../components/ui/control-tooltip';
import { ImageWithFallback } from '../../components/media/ImageWithFallback';
import { PromptEditor } from './PromptEditor';
import { AssetPicker } from './AssetPicker';
import { MediaGenerator } from '../../features/media/MediaGenerator';
import type { useProduction } from './useProduction';
import { useCatalogText } from '../../contexts/useCatalogText';
export function ShotWorkspace({
  shot,
  number,
  controller,
  picker,
  onPickerChange,
}: {
  shot: StoryShot;
  number: number;
  controller: ReturnType<typeof useProduction>;
  picker: boolean;
  onPickerChange: (open: boolean) => void;
}) {
  const { c } = useCatalogText();
  const { currentProject: p } = useWorkspace(),
    [selectionError, setSelectionError] = useState('');
  const d = controller.draft(shot),
    value = d.value,
    editable = controller.editable;
  if (!p) return null;
  const seen = new Set<string>();
  const referenced = (value.videoPrompt?.parts || []).filter(part => part.type === 'asset')
    .filter(ref => {
      const key = refKey(ref);
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    });
  const edit = (patch: Partial<typeof value>) => controller.change(shot, { ...value, ...patch });
  const unused = value.videoPrompt
    ? value.selectedAssets.filter(
        (ref) =>
          !value.videoPrompt!.parts.some((part) => part.type === 'asset' && sameRef(part, ref)),
      )
    : [];
  return (
    <div className="sb-video-workspace">
      <MediaGenerator
        kind="video"
        inline
        target={{ episodeId: controller.view!.episode.id, shotId: shot.id }}
        estimatedDurationSeconds={value.durationSeconds}
        hasVideo={!!shot.thumbnailUrl}
        onVideoTaskChange={() => void controller.read().catch(() => {})}
        disabled={
          controller.dirty(shot) || !shot.videoPrompt?.valid || !!controller.view?.activeRun
        }
      >
        {(actions) => (
          <section className="sb-video-composer" aria-label={c('视频提示词编辑区')}>
            <header className="sb-shot-heading">
              <h2 className="sb-shot-number" title={shot.action}>{c('分镜 {number}', { number: String(number).padStart(2, '0') })}</h2>
              <p className="sb-composer-hint">{c('输入 @ 引用本镜头资产')}</p>
            </header>
            <div className="sb-referenced-assets" role="group" aria-label={c('已引用素材')}>
              {referenced.map(ref => {
                const asset = p.assets.find(item => item.id === ref.assetId);
                const image = ref.lookId ? asset?.looks.find(look => look.id === ref.lookId)?.image : asset?.image;
                const name = asset ? assetLabel(p.assets, ref, c)
                  : shot.videoPrompt?.parts.find(part => part.type === 'asset' && sameRef(part, ref))?.label
                    || assetLabel(p.assets, ref, c);
                return <ControlTooltip key={refKey(ref)} label={name}>
                  <span className="sb-referenced-asset" tabIndex={0} role="img" aria-label={name}
                    data-asset-id={ref.assetId} data-look-id={ref.lookId}>
                    {image?.thumbnailUrl ? <ImageWithFallback src={image.thumbnailUrl} alt="" />
                      : <ImageIcon aria-hidden="true" />}
                  </span>
                </ControlTooltip>;
              })}
              <Button
                className="sb-add-material"
                variant="ghost"
                size="sm"
                aria-label={c('添加素材')}
                title={c('添加素材')}
                disabled={!editable}
                onClick={() => onPickerChange(true)}
              >
                <Plus />
              </Button>
            </div>
            <PromptEditor
              footer={actions}
              savedParts={shot.videoPrompt?.parts}
              parts={value.videoPrompt?.parts || []}
              selected={value.selectedAssets}
              assets={p.assets}
              disabled={!editable}
              onChange={(parts) => edit({ videoPrompt: parts.length ? { parts } : null })}
            />
            {shot.videoPrompt && !shot.videoPrompt.valid ? (
              <p className="media-muted">{c('提示词已失效，请编辑保存或重新生成。')}</p>
            ) : null}
            {unused.length ? (
              <p className="sb-notice" role="status">
                {c('尚未引用：{assets}。请插入 @ 引用或移除未使用的选择。', {
                  assets: unused.map((ref) => assetLabel(p.assets, ref, c)).join(c('、')),
                })}
              </p>
            ) : null}
            {selectionError ? (
              <p role="alert" className="jm-inline-error">
                {selectionError}
              </p>
            ) : null}
            {d.message ? (
              <div role="alert" className="sb-notice">
                {d.message}
                {d.conflict ? (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={!editable || d.sourceVersion !== controller.view?.sourceVersion}
                    onClick={() => controller.rebase(shot)}
                  >
                    {c('核对后采用最新版本继续编辑')}
                  </Button>
                ) : null}
              </div>
            ) : null}
            {d.conflict ? (
              <div className="sb-composer-footer">
                <Button
                  variant="warning"
                  size="sm"
                  disabled={controller.busy}
                  onClick={() => controller.discard(shot)}
                >
                  {c('放弃草稿')}
                </Button>
              </div>
            ) : null}
          </section>
        )}
      </MediaGenerator>
      <AssetPicker
        open={picker}
        onClose={() => onPickerChange(false)}
        assets={p.assets}
        selected={value.selectedAssets}
        disabled={!editable}
        onConfirm={(refs) => {
          const removed = value.selectedAssets.filter((ref) => !refs.some((r) => sameRef(r, ref))),
            problems: string[] = [];
          for (const ref of removed) {
            const positions = referenceLocations(value.videoPrompt?.parts || [], ref, c);
            if (positions.length)
              problems.push(
                c('{asset}：{positions}', {
                  asset: assetLabel(p.assets, ref, c),
                  positions: positions.join(c('、')),
                }),
              );
            if (
              value.dialogues.some((l) => l.speakerAssetId === ref.assetId) &&
              !refs.some((r) => r.assetId === ref.assetId)
            )
              problems.push(
                c('{asset}：仍是对白说话人', { asset: assetLabel(p.assets, ref, c) }),
              );
          }
          if (problems.length) {
            setSelectionError(
              c('无法移除仍被引用的选择。请先修改对应提示词或保留选择：{problems}', {
                problems: problems.join(c('；')),
              }),
            );
            return;
          }
          setSelectionError('');
          edit({ selectedAssets: refs });
        }}
      />
    </div>
  );
}
