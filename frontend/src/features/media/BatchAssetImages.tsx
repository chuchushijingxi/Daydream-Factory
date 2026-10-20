import { useEffect, useId, useRef, useState } from 'react';
import { projectMediaApi, type ImageBatchItem, type MediaCommand } from '../../api/project-media';
import { billingApi } from '../../api/billing-client';
import { ApiError } from '../../api/client';
import type { Asset, CharacterImageType, VisualTarget } from '../../api/types';
import { useWorkspace } from '../../state/WorkspaceContext';
import { workflow } from '../../domain/project-workflow';
import { useRead } from '../../pages/operations/controls';
import { formatInteger } from '../../pages/credits/credit-format';
import { Button } from '../../components/ui/button';
import { CreditStar } from '../../components/ui/credit-star';
import { FreeBadge } from '../../components/ui/free-badge';
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '../../components/ui/dialog';
import { useCatalogText } from '../../contexts/useCatalogText';
import { selectMediaOptions } from './MediaSettings';
import { ImageGenerationSettings, lockedCharacterRatio } from './ImageGenerationSettings';
import { useAssetImageTasks, imageTargetKey, imageTaskState } from './AssetImageTasks';
import { sessionBoundary } from '../../api/session-boundary';
import { mediaCreditEstimate } from './media-credit-estimate';

export function BatchAssetImages({ kind, assetId }: { kind: Asset['kind']; assetId?: string }) {
  const { c } = useCatalogText();
  const { currentProject: p, models, status } = useWorkspace();
  const tasks = useAssetImageTasks();
  const priceId = useId();
  const [open, setOpen] = useState(false),
    [imageType, setImageType] = useState<CharacterImageType>('closeup_main'),
    [ratio, setRatio] = useState(''),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [pending, setPending] = useState<MediaCommand[] | null>(null),
    [failed, setFailed] = useState<ImageBatchItem[] | null>(null);
  const scope = JSON.stringify([p?.id, kind, assetId]),
    storageKey = 'image-batch:' + scope;
  const current = useRef(scope),
    guard = useRef(new Set<string>()),
    alive = useRef(true);
  current.current = scope;
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    setOpen(false);
    setError('');
    setFailed(null);
    setBusy(false);
    setImageType('closeup_main');
    setRatio('');
    try {
      setPending(JSON.parse(sessionStorage.getItem(storageKey) || 'null'));
    } catch {
      setPending(null);
    }
  }, [storageKey]);
  const entries = (p?.assets || [])
    .filter((asset) => asset.kind === kind && (!assetId || asset.id === assetId))
    .flatMap((asset) =>
      assetId
        ? asset.looks.map((look) => ({
            target: { assetId: asset.id, lookId: look.id },
            name: look.name,
            image: look.image,
          }))
        : [{ target: { assetId: asset.id } as VisualTarget, name: asset.name, image: asset.image }],
    );
  const eligible = entries
    .filter(
      (entry) =>
        !entry.image &&
        !!p &&
        workflow.effectivePrompt(p, entry.target) &&
        !imageTaskState(tasks?.latest.get(imageTargetKey(entry.target)), entry.image).busy &&
        tasks?.latest.get(imageTargetKey(entry.target))?.status !== 'unknown',
    )
    .slice(0, 200);
  const activeCount = entries.filter(
    (entry) => imageTaskState(tasks?.latest.get(imageTargetKey(entry.target)), entry.image).busy,
  ).length;
  const model = models.image.find((option) => option.id === p?.settings?.imageModel);
  const selected = selectMediaOptions('image', model?.mediaProfile);
  const aspectRatio =
    kind === 'character' && lockedCharacterRatio(imageType)
      ? '16:9'
      : ratio || p?.settings?.aspectRatio || '1:1';
  const composition = { aspectRatio, ...(kind === 'character' ? { imageType } : {}) };
  const available =
    !!p &&
    workflow.catalogReady(p) &&
    !p.runs.some((run) => run.status === 'running' && run.contract !== 'storyboard-agent-v1') &&
    !!status?.capabilities.projectMedia &&
    !status.readOnly &&
    !!selected &&
    !!model &&
    (model.provider === 'mock' || model.id === 'mock-image');
  const unitPrice = mediaCreditEstimate(model, selected);
  // 限免模型只展示活动标签，不显示 0 积分。
  const free = model?.free === true;
  // 入口只展示已有模型目录的预计金额；确认仍必须取得每个目标的服务端计费依据。
  const entryAmount =
    unitPrice !== undefined ? BigInt(unitPrice) * BigInt(eligible.length) : undefined;
  const quoteKey = JSON.stringify([
    scope,
    p?.revision,
    model?.id,
    selected,
    composition,
    eligible.map((entry) => entry.target),
  ]);
  const quote = useRead(
    async (signal) => {
      const quoted: { target: VisualTarget; basis: string; amount: number }[] = [];
      // 只读报价有界并发，避免大目录同时创建数百个HTTP请求。
      for (let i = 0; i < eligible.length; i += 4) {
        signal.throwIfAborted();
        quoted.push(
          ...(await Promise.all(
            eligible.slice(i, i + 4).map(async (entry) => {
              const value = await billingApi.project(p!.id, {
                revision: p!.revision,
                operation: 'media',
                modelId: model!.id,
                target: entry.target,
                options: selected,
                ...composition,
              }, signal);
              return { target: entry.target, basis: value.basis, amount: value.amount };
            }),
          )),
        );
      }
      return quoted;
    },
    [quoteKey],
    open && available && !!tasks?.ready && !tasks.error && eligible.length > 0 && !busy && !pending,
  );
  const confirmedAmount = quote.value?.reduce((sum, value) => sum + BigInt(value.amount), 0n);
  const amount = confirmedAmount ?? entryAmount;
  const submit = async (retry = false) => {
    if (guard.current.has(scope) || !p || (!retry && (!quote.value || quote.busy || quote.error)))
      return;
    const commands = retry
      ? pending
      : quote.value!.map((item) => ({
          commandId: crypto.randomUUID(),
          revision: p.revision,
          modelId: model!.id,
          target: item.target,
          options: selected,
          ...composition,
          billingBasis: item.basis,
        }));
    if (!commands?.length) return;
    const at = scope,
      key = storageKey,
      projectId = p.id,
      epoch = sessionBoundary.epoch;
    guard.current.add(at);
    setBusy(true);
    setError('');
    setFailed(null);
    try {
      sessionStorage.setItem(key, JSON.stringify(commands));
      setPending(commands);
      const response = await projectMediaApi.batchImages(projectId, commands);
      if (epoch !== sessionBoundary.epoch) return;
      const uncertain = response.filter(
        (item) =>
          item.error?.code === 'PROGRAM_FAILED' || item.error?.code === 'CREDIT_COMMIT_UNKNOWN',
      );
      if (!uncertain.length) sessionStorage.removeItem(key);
      if (!alive.current || current.current !== at) return;
      setPending(uncertain.length ? commands : null);
      setFailed(response.filter((item) => item.error));
      setOpen(false);
      tasks?.refresh();
    } catch (e) {
      const rejected = e instanceof ApiError && e.status >= 400 && e.status < 500;
      if (epoch === sessionBoundary.epoch && rejected) {
        sessionStorage.removeItem(key);
        if (alive.current && current.current === at) setPending(null);
      }
      if (alive.current && current.current === at && epoch === sessionBoundary.epoch) {
        const message = (e as Error).message;
        setError(
          rejected ? message : c('{message}；请核对原请求，勿重复新建任务。', { message }),
        );
      }
    } finally {
      guard.current.delete(at);
      if (alive.current && current.current === at) setBusy(false);
    }
  };
  if (
    !p ||
    (!pending && !entries.some((entry) => !entry.image)) ||
    (!pending && !eligible.length && !failed?.length && !error && !tasks?.error)
  )
    return null;
  return (
    <>
      {pending || eligible.length > 0 || tasks?.error ? (
        <Button
          size="sm"
          className="asset-batch-entry"
          aria-label={pending ? c('核对批量请求') : c('批量生成图片')}
          aria-describedby={
            !pending && available && eligible.length > 0 && entryAmount !== undefined
              ? priceId
              : undefined
          }
          title={
            !available
              ? c('请先完成资产解析并选择可用的图片模型')
              : !tasks?.ready
                ? c('正在读取图片任务')
                : !eligible.length
                  ? c('没有可补图的资产')
                  : quote.error || undefined
          }
          disabled={
            !pending && (!available || !tasks?.ready || (!eligible.length && !tasks?.error))
          }
          onClick={() => {
            setError('');
            setOpen(true);
          }}
        >
          {pending ? c('核对批量请求') : c('批量生成图片')}
          {!pending && available && eligible.length > 0 && entryAmount !== undefined ? (
            <span
              id={priceId}
              className="asset-batch-price"
              aria-label={
                free
                  ? c('限免')
                  : c('预计消耗 {amount} 积分', {
                      amount: formatInteger(entryAmount.toString()),
                    })
              }
            >
              {free ? (
                <FreeBadge />
              ) : (
                <>
                  <CreditStar />
                  {formatInteger(entryAmount.toString())}
                </>
              )}
            </span>
          ) : null}
          {activeCount ? <small>{c('生成中 {count}', { count: activeCount })}</small> : null}
        </Button>
      ) : null}
      {failed?.length ? (
        <ul className="asset-batch-errors" role="alert">
          {failed.map((item) => (
            <li key={item.commandId}>
              {entries.find(
                (entry) =>
                  imageTargetKey(entry.target) === imageTargetKey(item.target as VisualTarget),
              )?.name || c('资产已不可用')}
              ：{item.error?.message}
            </li>
          ))}
        </ul>
      ) : null}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="asset-batch-dialog">
          <DialogTitle>
            {assetId
              ? c('批量生成附加形象')
              : kind === 'character'
                ? c('批量生成角色')
                : kind === 'location'
                  ? c('批量生成场景')
                  : c('批量生成道具')}
          </DialogTitle>
          <DialogDescription className="sr-only">{c('设置本次图片生成参数。')}</DialogDescription>
          {!pending ? (
            <>
              <ImageGenerationSettings
                imageType={kind === 'character' ? imageType : undefined}
                aspectRatio={aspectRatio}
                aspectRatios={model?.mediaProfile?.aspectRatios || []}
                disabled={busy}
                onImageTypeChange={setImageType}
                onAspectRatioChange={setRatio}
              />
              {quote.error ? (
                <p role="alert" className="jm-inline-error">
                  {quote.error}
                  <Button variant="link" onClick={quote.refresh}>
                    {c('重新读取价格')}
                  </Button>
                </p>
              ) : null}
            </>
          ) : null}
          {tasks?.error ? (
            <p role="alert" className="jm-inline-error">
              {c('任务状态读取失败：{error}', { error: tasks.error })}
              <Button variant="link" onClick={tasks.refresh}>
                {c('重新读取')}
              </Button>
            </p>
          ) : null}
          {error ? (
            <p role="alert" className="jm-inline-error">
              {error}
            </p>
          ) : null}
          {pending ? (
            <p className="media-warning">
              {busy
                ? c('正在提交，已接受任务会在后台继续。')
                : c('部分受理尚未确认，请核对原请求。')}
            </p>
          ) : null}
          <footer className="asset-batch-actions">
            <Button variant="outline" onClick={() => setOpen(false)}>
              {c('取消')}
            </Button>
            {pending ? (
              <Button
                busy={busy}
                disabled={busy || status?.readOnly}
                onClick={() => void submit(true)}
              >
                {c('核对原请求')}
              </Button>
            ) : (
              <Button
                aria-label={c('确认批量生成图片')}
                busy={busy}
                disabled={
                  !available ||
                  !quote.value?.length ||
                  quote.busy ||
                  !!quote.error ||
                  !!tasks?.error ||
                  busy
                }
                onClick={() => void submit()}
              >
                {c('生成图片')}{' '}
                {free ? (
                  <span className="asset-batch-price" aria-label={c('限免')}>
                    <FreeBadge />
                  </span>
                ) : (
                  <span
                    className="asset-batch-price"
                    aria-label={
                      amount === undefined
                        ? c('积分待确认')
                        : confirmedAmount === undefined
                          ? c('预计消耗 {amount} 积分', {
                              amount: formatInteger(amount.toString()),
                            })
                          : c('{amount} 积分', { amount: formatInteger(amount.toString()) })
                    }
                  >
                    <CreditStar />
                    {amount === undefined ? '…' : formatInteger(amount.toString())}
                  </span>
                )}
              </Button>
            )}
          </footer>
        </DialogContent>
      </Dialog>
    </>
  );
}
