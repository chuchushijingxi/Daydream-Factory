import { workflow } from '../../domain/project-workflow.ts';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Maximize, Minus, Network, Plus, X } from 'lucide-react';
import { TransformComponent, TransformWrapper } from 'react-zoom-pan-pinch';
import { ImageWithFallback } from '../../components/media/ImageWithFallback';
import { Button } from '../../components/ui/button';
import { ControlTooltip } from '../../components/ui/control-tooltip';
import { cn } from '../../components/ui/utils';
import { useWorkspace } from '../../state/WorkspaceContext';
import { ImageUnavailablePlaceholder } from '../../features/project/components/ImageUnavailablePlaceholder';
import { AssetDialog } from '../../features/project/components/AssetDialog';
import type { Look } from '../../api/types';
import { AssetImageTasksProvider, AssetImageTaskStatus } from '../../features/media/AssetImageTasks';
import { BatchAssetImages } from '../../features/media/BatchAssetImages';
type LookNode = {
	assetId: string;
  id: string;
  label: string;
  look?: Look;
  imageUrl?: string;
};
const LOOK_CARD_W_PX = 134,
  LOOK_GAP_PX = 24,
  LOOK_CARD_H_PX = 272,
  VERTICAL_CONNECTOR_H_PX = 72,
  CONNECTOR_COLOR = '#CBCBCA';
const CARD_IMAGE_BOTTOM_SCRIM =
  'linear-gradient(180deg, rgba(0, 0, 0, 0) 0%, rgba(0, 0, 0, 0.08) 12.11%, rgba(0, 0, 0, 0.8) 100%)';
function LookCard({
  node,
  variant = 'look',
  onImageClick,
  onAdd,
}: {
  node: LookNode;
  variant?: 'role' | 'look';
  onImageClick: () => void;
  onAdd?: () => void;
}) {
  const { c } = useCatalogText();
  const isRole = variant === 'role';
  const addLookControl = onAdd ? (
    <div className="flex w-full justify-center">
      <ControlTooltip label={c('新增附加形象')}>
        <Button variant="ghost" size="icon" onClick={onAdd} aria-label={c('新增附加形象')}>
          <Plus />
        </Button>
      </ControlTooltip>
    </div>
  ) : null;
  return (
    <div
      className={cn(
        'group/card flex shrink-0 flex-col items-center gap-2',
        isRole ? 'w-[264px]' : 'w-[134px]',
      )}
    >
      {isRole ? (
        <>
          <div className="group/image relative w-full overflow-hidden rounded-[12px]">
            <div className="relative">
              <button
                type="button"
                className={cn(
                  'relative block w-full overflow-hidden',
                  node.imageUrl ? 'bg-[#1a1a1a]' : 'bg-[#F5F6F7] dark:bg-slate-800/95',
                )}
                onClick={onImageClick}
              >
                {node.imageUrl ? (
                  <ImageWithFallback
                    src={node.imageUrl}
                    alt={node.label}
                    className="h-[198px] w-[264px] object-cover object-[50%_8%]"
                  />
                ) : (
                  <div className="flex h-[198px] w-[264px] items-center justify-center bg-[#F5F6F7] dark:bg-slate-800/95">
                    <ImageUnavailablePlaceholder />
                  </div>
                )}
                {node.imageUrl ? (
                  <div
                    className="pointer-events-none absolute inset-x-0 bottom-0 h-[62px]"
                    style={{
                      background: CARD_IMAGE_BOTTOM_SCRIM,
                    }}
                  >
                    <p className="absolute bottom-2 left-2 right-2 truncate text-left text-[10px] font-semibold text-white">
                      {node.label}
                    </p>
                  </div>
                ) : null}
              </button>
              <AssetImageTaskStatus target={{ assetId: node.assetId, ...(node.look ? { lookId: node.look.id } : {}) }} image={!!node.imageUrl} />
            </div>
          </div>
          <span className="text-[12px] text-playlet-title dark:text-white">{node.label}</span>
          {addLookControl}
        </>
      ) : (
        <>
          <div className="group/image relative w-full overflow-hidden rounded-xl border border-playlet-border bg-[#F5F6F7] dark:border-slate-600 dark:bg-slate-800/95">
            <div className="relative">
              <button
                type="button"
                className="relative block w-full cursor-pointer overflow-hidden p-0 outline-none focus-visible:ring-2 focus-visible:ring-[#3370ff] focus-visible:ring-offset-2 focus-visible:ring-offset-[#F5F6F7] dark:focus-visible:ring-violet-500 dark:focus-visible:ring-offset-slate-800"
                onClick={onImageClick}
              >
                {node.imageUrl ? (
                  <ImageWithFallback
                    src={node.imageUrl}
                    alt={node.label}
                    className="h-[240px] w-full min-w-0 object-cover object-[50%_8%]"
                  />
                ) : (
                  <div className="flex h-[240px] w-full min-w-0 items-center justify-center bg-[#0A1B3108] dark:bg-white/6">
                    <ImageUnavailablePlaceholder />
                  </div>
                )}
                {node.imageUrl ? (
                  <div
                    className="pointer-events-none absolute inset-x-0 bottom-0 h-[62px]"
                    style={{
                      background: CARD_IMAGE_BOTTOM_SCRIM,
                    }}
                  >
                    <div className="absolute bottom-2 left-2 right-2 flex flex-col gap-0.5">
                      <p className="truncate text-left text-[10px] font-semibold text-white">
                        {node.label}
                      </p>
                    </div>
                  </div>
                ) : null}
              </button>
              <AssetImageTaskStatus target={{ assetId: node.assetId, ...(node.look ? { lookId: node.look.id } : {}) }} image={node.look?.image} />
            </div>
          </div>
          <span className="text-[12px] text-playlet-title dark:text-white">{node.label}</span>
          {addLookControl}
        </>
      )}
    </div>
  );
}
export function CharacterLooksPage() {
  return <AssetImageTasksProvider><CharacterLooksContent /></AssetImageTasksProvider>;
}
function CharacterLooksContent() {
  const { c } = useCatalogText();
  const { id: playletId, characterId } = useParams();
  const navigate = useNavigate();
  const { currentProject: p, status } = useWorkspace();
  const [layoutVertical, setLayoutVertical] = useState(true);
  const canvasHostRef = useRef<HTMLDivElement>(null);
  const [dialog, setDialog] = useState<{
    node?: LookNode;
    create: boolean;
  } | null>(null);
  if (!p) return null;
  const roster = p.assets.filter((a) => a.kind === 'character'),
    active = roster.find((a) => a.id === characterId);
  if (!active) return <p className="p-6">{c('角色不存在或已删除。')}</p>;
  const editable = Boolean(status && !status.readOnly && workflow.state(p).editable);
  const mainLook: LookNode = {
      assetId: active.id,
      id: active.id,
      label: c('{name} · {label}', { name: active.name, label: c('基础') }),
      imageUrl: active.image?.thumbnailUrl,
    },
    childLooks: LookNode[] = active.looks.map((look) => ({
      assetId: active.id,
      id: look.id,
      label: look.name,
      imageUrl: look.image?.thumbnailUrl,
      look,
    }));
  const openLookDialog = (mode: 'create' | 'edit', node?: LookNode) =>
    setDialog({
      node,
      create: mode === 'create',
    });
  const toggleFullscreen = () => {
    if (document.fullscreenElement) void document.exitFullscreen();
    else void canvasHostRef.current?.requestFullscreen();
  };
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden bg-[#f5f5f5] text-playlet-title dark:bg-[#0a0a0a] dark:text-slate-100">
      {/* 顶栏 */}
      <header className="shrink-0 border-b border-playlet-border bg-white px-4 py-3 dark:border-slate-700 dark:bg-[#111]">
        <div className="mx-auto flex max-w-[1600px] flex-col gap-3">
          <div className="flex items-center justify-between gap-3">
            <div className="flex min-w-0 flex-1 flex-wrap gap-2">
              {roster.map((c) => (
                <button
                  key={c.id}
                  type="button"
                  onClick={() => navigate(`/playlet/${playletId}/characters/${c.id}/canvas`)}
                  className="jm-roster-tab"
                  aria-pressed={c.id === active.id}
                >
                  {c.name}
                </button>
              ))}
            </div>
            <ControlTooltip label={c('返回角色列表')}>
              <Button
                variant="ghost"
                className="jm-button--close"
                onClick={() => navigate(`/playlet/${playletId}/characters`)}
                aria-label={c('关闭角色画布')}
              >
                <X />
              </Button>
            </ControlTooltip>
          </div>

          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0 flex-1">
              <h1 className="text-[20px] font-bold text-playlet-title dark:text-white">
                {active.name}
              </h1>
              <p className="mt-2 text-[13px] leading-relaxed text-playlet-text-muted dark:text-white/75">
                {active.description}
              </p>
            </div>
            <BatchAssetImages kind="character" assetId={active.id} />
            <Button variant="outline" onClick={() => openLookDialog('edit', mainLook)}>
              {c('查看基础资料')}
            </Button>
          </div>
        </div>
      </header>

      {/* 画布区 */}
      <div ref={canvasHostRef} className="relative min-h-0 flex-1 bg-[#fafafa] dark:bg-[#0d0d0d]">
        <div className="jm-canvas-controls absolute right-4 top-4 z-20 flex gap-1 p-1">
          <ControlTooltip label={c('横向排列')}>
            <Button
              variant="ghost"
              size="icon"
              aria-label={c('横向排列')}
              aria-pressed={!layoutVertical}
              onClick={() => setLayoutVertical(false)}
            >
              <Network className="-rotate-90" />
            </Button>
          </ControlTooltip>
          <ControlTooltip label={c('纵向排列')}>
            <Button
              variant="ghost"
              size="icon"
              aria-label={c('纵向排列')}
              aria-pressed={layoutVertical}
              onClick={() => setLayoutVertical(true)}
            >
              <Network />
            </Button>
          </ControlTooltip>
        </div>

        <div className="absolute inset-0 min-h-0">
          <TransformWrapper
            initialScale={1}
            minScale={0.35}
            maxScale={2.5}
            centerOnInit
            limitToBounds={false}
            alignmentAnimation={{
              disabled: true,
            }}
            panning={{
              velocityDisabled: true,
            }}
            wheel={{
              step: 0.15,
            }}
            doubleClick={{
              disabled: true,
            }}
          >
            {({ zoomIn, zoomOut }) => (
              <>
                <div className="jm-canvas-controls absolute bottom-4 left-4 z-20 flex flex-col gap-1 p-1">
                  <ControlTooltip label={c('放大')}>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => zoomIn()}
                      aria-label={c('放大')}
                    >
                      <Plus />
                    </Button>
                  </ControlTooltip>
                  <ControlTooltip label={c('缩小')}>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => zoomOut()}
                      aria-label={c('缩小')}
                    >
                      <Minus />
                    </Button>
                  </ControlTooltip>
                  <ControlTooltip label={c('全屏')}>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={toggleFullscreen}
                      aria-label={c('全屏')}
                    >
                      <Maximize />
                    </Button>
                  </ControlTooltip>
                </div>

                <TransformComponent
                  wrapperClass="!h-full !w-full"
                  contentClass="!flex !min-h-[min(70vh,560px)] !w-full !items-center !justify-center !p-12"
                >
                  {layoutVertical ? (
                    <div className="flex flex-col items-center">
                      {mainLook && (
                        <LookCard
                          node={mainLook}
                          variant="role"
                          onImageClick={() => openLookDialog('edit', mainLook)}
                          onAdd={editable ? () => openLookDialog('create') : undefined}
                        />
                      )}
                      {childLooks.length > 0 && (
                        <div className="flex flex-col items-center">
                          <div
                            className="relative"
                            style={{
                              width: `${childLooks.length * LOOK_CARD_W_PX + (childLooks.length - 1) * LOOK_GAP_PX}px`,
                              height: VERTICAL_CONNECTOR_H_PX,
                            }}
                          >
                            <svg
                              className="absolute inset-0"
                              width="100%"
                              height="100%"
                              viewBox={`0 0 ${childLooks.length * LOOK_CARD_W_PX + (childLooks.length - 1) * LOOK_GAP_PX} ${VERTICAL_CONNECTOR_H_PX}`}
                              fill="none"
                              aria-hidden
                            >
                              {childLooks.map((_, index) => {
                                const totalWidth =
                                  childLooks.length * LOOK_CARD_W_PX +
                                  (childLooks.length - 1) * LOOK_GAP_PX;
                                const rootX = totalWidth / 2;
                                const childX =
                                  index * (LOOK_CARD_W_PX + LOOK_GAP_PX) + LOOK_CARD_W_PX / 2;
                                const endY = VERTICAL_CONNECTOR_H_PX - 4;
                                const midY = Math.round(VERTICAL_CONNECTOR_H_PX * 0.55);
                                return (
                                  <path
                                    key={`v-link-${index}`}
                                    d={`M ${rootX} 2 C ${rootX} ${Math.round(VERTICAL_CONNECTOR_H_PX * 0.35)}, ${childX} ${midY}, ${childX} ${endY}`}
                                    stroke={CONNECTOR_COLOR}
                                    strokeWidth="1"
                                    strokeLinecap="round"
                                  />
                                );
                              })}
                            </svg>
                          </div>
                          <div className="relative flex flex-row flex-nowrap justify-center gap-6">
                            {childLooks.map((node) => (
                              <div key={node.id} className="flex flex-col items-center">
                                <LookCard
                                  node={node}
                                  onImageClick={() => openLookDialog('edit', node)}
                                  onAdd={editable ? () => openLookDialog('create') : undefined}
                                />
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  ) : (
                    <div className="flex flex-row items-center justify-center">
                      {mainLook && (
                        <div className="flex shrink-0 flex-col justify-center">
                          <LookCard
                            node={mainLook}
                            variant="role"
                            onImageClick={() => openLookDialog('edit', mainLook)}
                            onAdd={editable ? () => openLookDialog('create') : undefined}
                          />
                        </div>
                      )}
                      {childLooks.length > 0 && (
                        <div className="flex min-h-0 flex-row items-center">
                          <div
                            className="relative mr-4"
                            style={{
                              width: '88px',
                              height: `${childLooks.length * LOOK_CARD_H_PX + (childLooks.length - 1) * LOOK_GAP_PX}px`,
                            }}
                          >
                            <svg
                              className="absolute inset-0"
                              width="100%"
                              height="100%"
                              viewBox={`0 0 88 ${childLooks.length * LOOK_CARD_H_PX + (childLooks.length - 1) * LOOK_GAP_PX}`}
                              fill="none"
                              aria-hidden
                            >
                              {childLooks.map((_, index) => {
                                const totalHeight =
                                  childLooks.length * LOOK_CARD_H_PX +
                                  (childLooks.length - 1) * LOOK_GAP_PX;
                                const rootY = totalHeight / 2;
                                const childY =
                                  index * (LOOK_CARD_H_PX + LOOK_GAP_PX) + LOOK_CARD_H_PX / 2;
                                return (
                                  <path
                                    key={`h-link-${index}`}
                                    d={`M 2 ${rootY} C 28 ${rootY}, 58 ${childY}, 86 ${childY}`}
                                    stroke={CONNECTOR_COLOR}
                                    strokeWidth="2"
                                    strokeLinecap="round"
                                  />
                                );
                              })}
                            </svg>
                          </div>
                          <div className="flex flex-col justify-center gap-6">
                            {childLooks.map((node) => (
                              <div key={node.id} className="flex flex-row items-center">
                                <LookCard
                                  node={node}
                                  onImageClick={() => openLookDialog('edit', node)}
                                  onAdd={editable ? () => openLookDialog('create') : undefined}
                                />
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  )}
                </TransformComponent>
              </>
            )}
          </TransformWrapper>
        </div>
      </div>
      <AssetDialog
        open={Boolean(dialog)}
        onOpenChange={(v) => {
          if (!v) setDialog(null);
        }}
        kind="character"
        asset={active}
        look={dialog?.node?.look}
        createLook={dialog?.create}
      />
    </div>
  );
}
