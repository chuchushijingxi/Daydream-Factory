import { protectedResourceUrl } from '../../api/client';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useEffect, useId, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { Clapperboard, ImagePlus, LoaderCircle, Play, History, CircleAlert } from 'lucide-react';
import { CreditStar } from '../../components/ui/credit-star';
import { FreeBadge } from '../../components/ui/free-badge';
import { Spinner } from '../../components/ui/spinner';
import { MediaSettings, selectMediaOptions } from './MediaSettings';
import type { CharacterImageType, MediaOptions } from '../../api/types';
import { ImageGenerationSettings, lockedCharacterRatio } from './ImageGenerationSettings';
import type { Project } from '../../api/types';
import type { MediaTarget, ProjectMedia } from '../../api/project-media';
import { useProjectMedia } from './useProjectMedia';
import { useWorkspace } from '../../state/WorkspaceContext';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '../../components/ui/dialog';
import { Choice } from '../../pages/production/Choice';
import { billingApi } from '../../api/billing-client';
import { useChargeQuote, ChargeNotice } from '../billing/useChargeQuote';
import { formatInteger } from '../../pages/credits/credit-format';
import { VideoVersionsDialog, videoVersions } from './VideoVersionsDialog';
import { useAssetImageTasks } from './AssetImageTasks';
import { mediaCreditEstimate } from './media-credit-estimate';
import { sessionBoundary } from '../../api/session-boundary';
const labels: Record<ProjectMedia['status'], string> = {
  queued: '排队中',
  running: '正在生成',
  unknown: '结果待确认',
  succeeded: '已完成',
  failed: '生成失败',
  cancelled: '已取消',
};

export function MediaGenerator({
  target,
  kind,
  disabled = false,
  inline = false,
  children,
  title,
  renderEntry,
  onAdopt,
  onVideoTaskChange,
  estimatedDurationSeconds,
  hasVideo = false,
}: {
  onAdopt?: (project: Project, expectedRevision: number) => void;
  onVideoTaskChange?: () => void;
  estimatedDurationSeconds?: number | null;
  hasVideo?: boolean;
  renderEntry?: (
    open: () => void,
    entry: { label: string; price: ReactNode; priceId?: string },
  ) => ReactNode;
  title?: string;
  children?: (actions: ReactNode) => ReactNode;
  target: MediaTarget;
  kind: 'image' | 'video';
  disabled?: boolean;
  inline?: boolean;
}) {
  const { c } = useCatalogText();
  const { currentProject: p, models, status } = useWorkspace();
  const imageTasks = useAssetImageTasks();
  const imageCallback = useRef(imageTasks?.refresh);
  imageCallback.current = imageTasks?.refresh;
  const [open, setOpen] = useState(inline),
    [model, setModel] = useState(''),
    [preview, setPreview] = useState(''),
    [failedVideo, setFailedVideo] = useState(''),
    [videoRetry, setVideoRetry] = useState(0),
    [versionsOpen, setVersionsOpen] = useState(false);
  const media = useProjectMedia(target, open || inline, onAdopt),
    options = models[kind];
  const preferred = kind === 'image' ? p?.settings?.imageModel : p?.settings?.videoModel;
  const inlineVideo = inline && kind === 'video';
  const chosen = inlineVideo
    ? preferred || ''
    : model || options.find((m) => m.id === preferred)?.id || options[0]?.id || '';
  const profile = options.find((m) => m.id === chosen)?.mediaProfile;
  const [settings, setSettings] = useState<Record<string, MediaOptions>>({});
  const [imageTypes, setImageTypes] = useState<Record<string, CharacterImageType>>({});
  const [imageRatios, setImageRatios] = useState<Record<string, string>>({});
  const settingsKey = JSON.stringify([
    p?.id,
    target,
    chosen,
    kind === 'video' ? estimatedDurationSeconds : null,
  ]);
  const selected = selectMediaOptions(
    kind,
    profile,
    kind === 'image' ? undefined : settings[settingsKey],
    kind === 'video' ? estimatedDurationSeconds : undefined,
  );
  const asset = 'assetId' in target ? p?.assets.find((a) => a.id === target.assetId) : null;
  const imageType =
    kind === 'image' && asset?.kind === 'character'
      ? imageTypes[settingsKey] || 'closeup_main'
      : undefined;
  const aspectRatio =
    kind === 'image'
      ? lockedCharacterRatio(imageType)
        ? '16:9'
        : imageRatios[settingsKey] || p?.settings?.aspectRatio || '1:1'
      : undefined;
  const supported =
    !!selected &&
    options.some(
      (m) =>
        m.id === chosen &&
        (m.provider === 'mock' || m.id === 'mock-image' || m.id === 'mock_video'),
    );
  const frozen = media.active || media.pending;
  const parameters = (
    <MediaSettings
      kind={kind}
      profile={profile}
      value={selected}
      frozen={frozen ? frozen.options || {} : undefined}
      disabled={media.busy || !!frozen}
      onChange={(next) => setSettings((old) => ({ ...old, [settingsKey]: next }))}
    />
  );
  const quoteKey = JSON.stringify([
    p?.id, p?.revision, chosen, target, selected, imageType, aspectRatio, open, media.billingSequence,
  ]);
  const quote = useChargeQuote(
    () =>
      billingApi.project(p!.id, {
        revision: p!.revision,
        operation: 'media',
        modelId: chosen,
        target,
        options: selected,
        ...(imageType ? { imageType } : {}),
        ...(aspectRatio ? { aspectRatio } : {}),
      }),
    quoteKey,
    !!p &&
      (open || inline) &&
      supported &&
      !disabled &&
      media.available &&
      !media.active &&
      !media.pending,
  );
  const adopted = media.items.find((t) => t.adoptedOutputId),
    latest = media.items.find((t) => t.status === 'succeeded' && t.result?.files.length),
    shown = adopted || latest;
  const taskCallback = useRef(onVideoTaskChange);
  taskCallback.current = onVideoTaskChange;
  const latestTask = media.items[0];
  useEffect(() => {
    if (kind === 'video' && latestTask) taskCallback.current?.();
    if (kind === 'image' && latestTask) imageCallback.current?.();
  }, [kind, latestTask?.id, latestTask?.revision]);
  const output =
    shown?.result?.files.find((f) => f.id === shown.adoptedOutputId) || shown?.result?.files[0];
  const look = asset && 'lookId' in target ? asset.looks.find((l) => l.id === target.lookId) : null;
  const targetImage = asset && 'lookId' in target && target.lookId ? look?.image : asset?.image;
  const imageAction = targetImage ? '重新生成' : '生成图片';
  const selectedModel = options.find((m) => m.id === chosen);
  const entryAmount =
    kind === 'image'
      ? mediaCreditEstimate(selectedModel, selected)
      : undefined;
  const entryPriceId = useId();
  // 限免模型或限免报价都不显示 0 积分，改展示“限免”活动标签。
  const entryFree =
    entryAmount !== undefined && (selectedModel?.free === true || !!quote.value?.free);
  const entryPrice =
    entryFree ? (
      <span id={entryPriceId} className="button-credit-amount" aria-label={c('限免')}>
        <FreeBadge />
      </span>
    ) : entryAmount !== undefined ? (
      <span
        id={entryPriceId}
        className="button-credit-amount"
        aria-label={c('预计消耗 {amount} 积分', { amount: entryAmount })}
      >
        <CreditStar />
        {formatInteger(String(entryAmount))}
      </span>
    ) : null;
  const imageTitle = asset
    ? look
      ? asset.name + ' · ' + look.name
      : asset.kind === 'character'
        ? c('{name} · 基础', { name: asset.name })
        : asset.name
    : c('资产图片');
  const versions = videoVersions(media.items),
    versionIndex = versions.findIndex((v) => v.file.id === output?.id);
  const priceId = useId();
  const videoFailureTitleId = useId();
  const failedTask = inlineVideo && !media.active && latestTask?.status === 'failed' ? latestTask : null;
  const retryVideo = inlineVideo && !!media.pending;
  const videoAction = retryVideo && !media.busy
    ? '重试'
    : hasVideo || !!output
      ? '重新生成'
      : '生成视频';
  // 核价中立即显示当前模型/档位/秒数的预计金额；提交只使用本次服务端basis。
  // 不复用上一镜头金额，也不把禁用或未确认当成零价。
  const generationAmount = quote.value?.amount ?? (kind === 'image'
    ? entryAmount
    : selected?.durationSeconds !== undefined
      ? mediaCreditEstimate(options.find((m) => m.id === chosen), selected, selected.durationSeconds)
      : undefined);
  const buttonAmount = generationAmount === undefined ? '…' : formatInteger(String(generationAmount));
  const buttonFree = !!quote.value?.free || selectedModel?.free === true;
  const [startIntent, setStartIntent] = useState<{ key: string; epoch: number; amount: number } | null>(null),
    [priceReview, setPriceReview] = useState('');
  const intentGuard = useRef(false);
  const epoch = sessionBoundary.epoch;
  const unavailable = !media.available || media.busy || !!media.active ||
    (!retryVideo && (disabled || !supported || !!media.pending));
  const canAwaitQuote = quote.required && quote.busy && generationAmount !== undefined;
  const generationDisabled = unavailable || !!startIntent ||
    (!retryVideo && quote.blocked && !canAwaitQuote);
  useEffect(() => {
    if (!startIntent) return;
    const current = startIntent.key === quoteKey && startIntent.epoch === epoch && !unavailable;
    if (current && quote.busy) return;
    intentGuard.current = false;
    setStartIntent(null);
    if (!current || quote.blocked || !quote.value) return;
    // 只接续用户刚点击的同一目标；价格不同先展示新金额，不能按旧预计价格默默扣款。
    if (quote.value.amount !== startIntent.amount) {
      setPriceReview(quoteKey);
      return;
    }
    void media.submit(chosen, quote.basis, false, selected, imageType, aspectRatio);
  }, [startIntent, quoteKey, epoch, unavailable, quote.busy, quote.blocked, quote.value]);
  const generate = () => {
    if (generationDisabled || intentGuard.current) return;
    setPriceReview('');
    if (!retryVideo && canAwaitQuote && generationAmount !== undefined) {
      intentGuard.current = true;
      setStartIntent({ key: quoteKey, epoch, amount: generationAmount });
      return;
    }
    // 未明请求只核对已保存的原命令，不能重新报价后创建另一笔扣费。
    void media.submit(chosen, quote.basis, retryVideo, selected, imageType, aspectRatio);
  };
  const priceReviewNotice = priceReview === quoteKey ? (
    <p className="media-muted" role="status">{c('积分价格已更新，请确认后再次生成。')}</p>
  ) : null;
  const buttonPrice =
    (inlineVideo || kind === 'image') && quote.required && !media.active && !media.pending ? (
      <span
        id={priceId}
        className="media-button-price"
        aria-label={
          buttonFree
            ? c('限免')
            : generationAmount === undefined
              ? c('积分待确认')
              : quote.value
                ? c('{amount} 积分', { amount: buttonAmount })
                : c('预计消耗 {amount} 积分', { amount: buttonAmount })
        }
      >
        {buttonFree ? (
          <FreeBadge />
        ) : (
          <>
            <CreditStar />
            <span className="media-button-amount">{buttonAmount}</span>
          </>
        )}
      </span>
    ) : null;
  const generateButton = (
    <Button
      className={inlineVideo ? 'media-generate-video-button' : undefined}
      aria-label={inlineVideo ? c(videoAction) : kind === 'image' ? c(imageAction) : undefined}
      aria-describedby={buttonPrice ? priceId : undefined}
      size="sm"
      variant={inlineVideo || kind === 'image' ? 'default' : 'accent'}
      disabled={generationDisabled}
      busy={media.busy || !!startIntent}
      onClick={generate}
    >
      {inlineVideo ? null : media.active ? (
        <LoaderCircle className="animate-spin" />
      ) : kind === 'image' ? null : (
        <Clapperboard />
      )}
      {inlineVideo
        ? c(videoAction)
        : media.active
          ? c(labels[media.active.status])
          : kind === 'image'
            ? c(imageAction)
            : c(videoAction)}
      {buttonPrice}
    </Button>
  );
  const videoPreview = (
    <div
      className="media-video-preview"
      style={
        inlineVideo
          ? ({
              '--media-preview-ratio': p?.settings?.aspectRatio?.replace(':', '/') || '16 / 9',
            } as CSSProperties)
          : undefined
      }
    >
      {output ? (
        <video
          key={output.id + videoRetry}
          onError={() => setFailedVideo(output.id)}
          src={protectedResourceUrl(output.contentUrl)}
          poster={output.thumbnailUrl ? protectedResourceUrl(output.thumbnailUrl) : undefined}
          controls
          preload="metadata"
          playsInline
          aria-label={c('镜头视频预览')}
        />
      ) : inlineVideo && (media.active || failedTask) ? null : (
        <div className="media-preview-empty">
          {media.active ? <LoaderCircle className="animate-spin" /> : <Play />}
          <span>{media.active ? c(labels[media.active.status]) : c('等待生成视频')}</span>
          {media.active ? <small>{c('离开页面后任务仍会继续')}</small> : null}
        </div>
      )}
      {inlineVideo && media.active ? (
        <div
          className="media-video-progress"
          data-has-output={!!output}
          data-status={media.active.status}
          role="status"
          aria-label={c('视频生成进度')}
          aria-busy={media.active.status !== 'unknown'}
        >
          {media.active.status === 'unknown' ? (
            <CircleAlert className="media-video-pending-icon" aria-hidden />
          ) : (
            <span
              className="media-video-activity"
              role="progressbar"
              aria-label={c(labels[media.active.status])}
            >
              <Spinner />
            </span>
          )}
          <strong>{c(labels[media.active.status])}</strong>
          <Button
            size="sm"
            variant="ghost"
            disabled={media.busy || status?.readOnly}
            onClick={() => void media.cancel(media.active!)}
          >
            {c('取消生成')}
          </Button>
        </div>
      ) : null}
      {failedTask ? (
        <div
          className="media-video-error"
          data-has-output={!!output}
          role="alert"
          aria-labelledby={videoFailureTitleId}
        >
          <div className="media-video-error-copy">
            <h3 id={videoFailureTitleId}>
              <CircleAlert aria-hidden="true" />
              {output ? c('本次生成失败') : c('生成失败')}
            </h3>
            {failedTask.error ? <p>{failedTask.error}</p> : null}
          </div>
        </div>
      ) : null}
      {inlineVideo && (hasVideo || versions.length > 0) ? (
        <button
          type="button"
          className="media-version-badge"
          aria-label={c('选择视频')}
          aria-haspopup="dialog"
          title={
            media.items[0]?.status === 'failed'
              ? c('上次生成失败，点击查看视频')
              : c('选择视频')
          }
          onClick={() => setVersionsOpen(true)}
        >
          {versions.length ? (
            `${versionIndex < 0 ? 0 : versions.length - versionIndex}/${versions.length}${media.cursor ? '+' : ''}`
          ) : (
            <History />
          )}
        </button>
      ) : null}
    </div>
  );
  const content = (
    <section
      className={
        'media-generator media-generator-' + kind + (inlineVideo ? ' media-shot-generator' : '')
      }
      aria-label={kind === 'image' ? c('图片生成') : c('视频制作')}
    >
      {inlineVideo ? (
        <>
          <div className="media-video-layout">
            <div className="media-composer-column">
              {children ? (
                children(<div className="media-prompt-actions">{generateButton}</div>)
              ) : (
                <div className="media-prompt-actions">{generateButton}</div>
              )}
            </div>
            <section className="media-preview-column" aria-label={c('视频预览区')}>
              <div className="media-preview-heading">
                <h2>{c('视频预览')}</h2>
                {parameters}
              </div>
              <div className="media-preview-stage">{videoPreview}</div>
            </section>
          </div>
          {priceReviewNotice}
          {quote.error && !media.active && !media.pending ? (
            <p role="alert" className="jm-inline-error">
              {quote.error}
              <Button size="sm" variant="link" onClick={quote.refresh}>
                {c('重新读取价格')}
              </Button>
            </p>
          ) : null}
          {!supported ? (
            <p className="media-muted">
              {profile?.durations?.length &&
              estimatedDurationSeconds != null &&
              estimatedDurationSeconds > Math.max(...profile.durations)
                ? c(
                    '分镜时长 {seconds} 秒超过当前模型支持范围，请调整分镜或更换视频模型。',
                    { seconds: Math.ceil(estimatedDurationSeconds) },
                  )
                : preferred
                  ? c('当前视频模型尚未接入生成，请在右上角选择可用的 Mock 视频模型。')
                  : c('请在右上角选择视频模型。')}
            </p>
          ) : null}
        </>
      ) : kind === 'image' ? (
        <>
          <ImageGenerationSettings
            imageType={
              imageType ? (frozen ? frozen.imageType || 'no_preset' : imageType) : undefined
            }
            aspectRatio={frozen ? frozen.aspectRatio || '' : aspectRatio!}
            aspectRatios={profile?.aspectRatios || []}
            disabled={media.busy || !!frozen}
            onImageTypeChange={(next) => setImageTypes((old) => ({ ...old, [settingsKey]: next }))}
            onAspectRatioChange={(next) =>
              setImageRatios((old) => ({ ...old, [settingsKey]: next }))
            }
          />
          {!supported ? (
            <p className="media-muted">{c('请先在项目参数中选择可用的图片模型。')}</p>
          ) : null}
          {!media.active && !media.pending && (quote.busy || quote.error) ? (
            <ChargeNotice quote={quote} />
          ) : null}
          {priceReviewNotice}
          <footer className="asset-batch-actions">
            {!inline ? (
              <Button variant="outline" onClick={() => setOpen(false)}>
                {c('取消')}
              </Button>
            ) : null}
            {generateButton}
          </footer>
          {children?.(generateButton)}
        </>
      ) : (
        <>
          <div className="media-generation-toolbar">
            <div className="media-mode">
              <span>{title || c('镜头视频')}</span>
              <small>{c('本地模拟')}</small>
            </div>
            <span />
            {options.length ? (
              <Choice
                label={c('视频生成模型')}
                value={chosen}
                items={options.map((m) => ({ id: m.id, label: m.name }))}
                disabled={media.busy || !!media.active}
                onChange={setModel}
              />
            ) : (
              <span className="media-muted">{c('请在模型配置中启用 Mock')}</span>
            )}
            {generateButton}
          </div>
          {parameters}
          {!media.active && !media.pending ? (
            <div className="media-start-price">
              <ChargeNotice quote={quote} />
              {priceReviewNotice}
            </div>
          ) : null}
          {kind === 'video' ? videoPreview : null}
          {children?.(generateButton)}
        </>
      )}
      {output && failedVideo === output.id ? (
        <p role="alert" className="jm-inline-error">
          {c('视频读取失败，生成记录保留。')}
          <Button
            size="sm"
            variant="link"
            onClick={() => {
              setFailedVideo('');
              setVideoRetry((v) => v + 1);
            }}
          >
            {c('重新加载视频')}
          </Button>
        </p>
      ) : null}
      {kind === 'image' && media.items[0]?.status === 'failed' ? (
        <p role="alert" className="jm-inline-error">
          {media.items[0].error || c('生成失败')}
        </p>
      ) : null}
      {media.active && !inlineVideo ? (
        <div role="status" className="media-live" data-status={media.active.status}>
          <LoaderCircle className="animate-spin" />
          <span>
            {media.active.status === 'unknown'
              ? c('{status}，保留原任务，请勿重复提交', {
                  status: c(labels[media.active.status]),
                })
              : c('{status}，结果保存后自动显示', {
                  status: c(labels[media.active.status]),
                })}
          </span>
          <Button
            size="sm"
            variant="outline"
            disabled={media.busy || status?.readOnly}
            onClick={() => void media.cancel(media.active!)}
          >
            {c('取消生成')}
          </Button>
        </div>
      ) : null}
      {media.error ? (
        <p role="alert" className="jm-inline-error">
          {media.error}
          <Button size="sm" variant="link" onClick={() => void media.read().catch(() => {})}>
            {c('重新读取')}
          </Button>
        </p>
      ) : null}
      {media.pending && !inlineVideo ? (
        <p className="media-warning">
          {c('上次受理尚未确认。')}
          <Button
            size="sm"
            variant="link"
            disabled={media.busy}
            onClick={() => void media.submit(chosen, undefined, true)}
          >
            {c('核对原命令')}
          </Button>
        </p>
      ) : null}
      {!status?.capabilities.projectMedia ? (
        <p role="status" className="media-warning">
          {c('当前服务尚未启用项目媒体任务，请更新后端后重试。')}
        </p>
      ) : null}
    </section>
  );
  return (
    <>
      {!inline &&
        (renderEntry ? (
          renderEntry(() => setOpen(true), {
            label: imageAction,
            price: entryPrice,
            priceId: entryPrice ? entryPriceId : undefined,
          })
        ) : (
          <Button
            className="media-image-entry"
            variant="outline"
            size="sm"
            aria-label={c(imageAction)}
            aria-describedby={entryPrice ? entryPriceId : undefined}
            onClick={() => setOpen(true)}
          >
            <ImagePlus />
            {c(imageAction)}
            {entryPrice}
          </Button>
        ))}
      {inline ? (
        content
      ) : (
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogContent
            className={
              'media-generation-dialog' + (kind === 'image' ? ' media-image-generation-dialog' : '')
            }
          >
            <DialogTitle>
              {kind === 'image'
                ? asset?.kind === 'character'
                  ? look
                    ? c('生成附加形象')
                    : c('生成角色')
                  : asset?.kind === 'location'
                    ? c('生成场景')
                    : c('生成道具')
                : imageTitle}
            </DialogTitle>
            <DialogDescription>{imageTitle}</DialogDescription>
            {content}
          </DialogContent>
        </Dialog>
      )}
      {inlineVideo ? (
        <VideoVersionsDialog
          open={versionsOpen}
          onOpenChange={setVersionsOpen}
          media={media}
          currentOutputId={output?.id}
          onPreview={setPreview}
        />
      ) : null}
      <Dialog
        open={!!preview}
        onOpenChange={(v) => {
          if (!v) setPreview('');
        }}
      >
        <DialogContent variant="preview" className="media-preview-dialog">
          <DialogTitle className="sr-only">{c('生成结果预览')}</DialogTitle>
          <DialogDescription className="sr-only">{c('完整原始媒体')}</DialogDescription>
          {preview ? (
            kind === 'image' ? (
              <img src={protectedResourceUrl(preview)} alt={c('生成原图')} />
            ) : (
              <video src={protectedResourceUrl(preview)} controls autoPlay playsInline />
            )
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  );
}
