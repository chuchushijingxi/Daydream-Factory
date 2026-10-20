import { ChargeNotice, QuoteAmount } from '../../billing/useChargeQuote';
import { workflow } from '../../../domain/project-workflow.ts';
import { useCatalogText, workflowCopy } from '../../../contexts/useCatalogText';
import { Info, CheckCircle2 } from 'lucide-react';
import { useAssetPreparation } from '../useAssetPreparation';
import { Button } from '../../../components/ui/button';
import { ControlTooltip } from '../../../components/ui/control-tooltip';
import { Spinner } from '../../../components/ui/spinner';
export function ProjectTaskActions({ statusOnly = false }: { statusOnly?: boolean }) {
  const { c } = useCatalogText();
  const {
    project: p,
    flow,
    readonly,
    task,
    existing,
    pending,
    emptyVisual,
    selectionMissing,
    modelMissing,
    taskAvailable,
    quote,
    blocked,
    sending,
    error,
    readback,
    reload,
    start,
    cancel,
  } = useAssetPreparation();
  if (!p) return null;
  const copy = workflowCopy(flow, c);
  if (statusOnly && !flow.failed && !flow.cancelled && !flow.active && !error) return null;
  const message = copy.reason
    ? c('{label}：{reason}', { label: copy.label, reason: copy.reason })
    : flow.active
      ? c('{label}：{reason}', {
          label: copy.label,
          reason: readonly ? c('真实项目只读浏览') : copy.editReason,
        })
      : readonly
        ? c('真实项目只读浏览')
        : copy.editReason || copy.label;
  const label = task === 'visual' ? c('完善资产') : c(existing ? '继续生成资产' : '生成资产');
  const needsGeneration = task === 'extraction' || pending > 0 || emptyVisual;
  if (!message && !needsGeneration && !flow.active && !error) return null;
  const buttonDisabled = flow.active ? readonly || sending || readback : blocked || modelMissing;
  const buttonReason = readonly
    ? c('当前服务只读。')
    : readback
      ? c('先读取最新版本核对任务状态。')
      : !taskAvailable
        ? c('资产完善接口尚未接入。')
        : selectionMissing && !flow.active
          ? c('请先选择项目的分析模型。')
          : modelMissing && !flow.active
            ? c('暂无可用分析模型，请在模型配置中启用。')
            : task === 'visual' && pending === 0 && p.assets.length
              ? c('当前资产均已有有效提示词，可在详情中单项重新生成。')
              : label;
  return (
    <div className="asset-task-toolbar" aria-label={c('任务状态')}>
      {message ? (
        <ControlTooltip label={message}>
          <span
            className="asset-task-message jm-status"
            data-tone={
              flow.active
                ? 'info'
                : flow.failed || error
                  ? 'danger'
                  : flow.cancelled || readback
                    ? 'warning'
                    : flow.editable && pending === 0
                      ? 'success'
                      : 'neutral'
            }
            role={copy.reason ? 'alert' : 'status'}
            tabIndex={0}
          >
            {flow.active ? (
              <Spinner />
            ) : flow.editable && pending === 0 && !copy.reason ? (
              <CheckCircle2 aria-hidden />
            ) : (
              <Info aria-hidden />
            )}
            <span>{message}</span>
          </span>
        </ControlTooltip>
      ) : null}
      {flow.active ||
      (!statusOnly &&
        needsGeneration &&
        !(flow.failed && flow.failed.contract !== workflow.contracts.extraction) &&
        !(flow.cancelled && flow.cancelled.contract !== workflow.contracts.extraction)) ? (
        <ControlTooltip label={buttonReason} disabled={buttonDisabled}>
          <Button
            variant={flow.active ? 'warning' : 'accent'}
            size="sm"
            disabled={buttonDisabled}
            busy={sending}
            onClick={() => void (flow.active ? cancel() : start())}
          >
            {flow.active ? (
              c('取消任务')
            ) : (
              <>
                {label} <QuoteAmount quote={quote} />
              </>
            )}
          </Button>
        </ControlTooltip>
      ) : null}
      {!flow.active && needsGeneration ? <ChargeNotice quote={quote} /> : null}
      {error ? (
        <span role="alert" className="jm-inline-error asset-task-error">
          {error}
          {readback ? (
            <Button variant="link" size="sm" disabled={sending} onClick={() => void reload()}>
              {c('读取最新版本')}
            </Button>
          ) : null}
        </span>
      ) : null}
    </div>
  );
}
