import { useCatalogText } from '../../../contexts/useCatalogText';
import type { ReactNode } from 'react';
import { CreditStar } from '../../../components/ui/credit-star';
import { formatInteger } from '../../../pages/credits/credit-format';
import { Button } from '../../../components/ui/button';
import { cn } from '../../../components/ui/utils';
import { ArrowLeft, ArrowRight, Check, CircleAlert, Info } from 'lucide-react';
import { Spinner } from '../../../components/ui/spinner';
import type { WorkflowNotice } from '../workflow-footer-state';
type ProjectWorkflowFooterProps = {
  /** 左侧自定义内容，优先级高于 tip */
  leftContent?: ReactNode;
  /** 不传或空字符串时不展示左侧提示 */
  tip?: string;
  tipTone?: WorkflowNotice['tone'];
  onPrevious?: () => void;
  onNext?: () => void;
  nextDisabled?: boolean;
  nextBusy?: boolean;
  nextAmount?: number;
  className?: string;
};
export function ProjectWorkflowFooter({
  leftContent,
  tip,
  tipTone = 'neutral',
  onPrevious,
  onNext,
  nextDisabled = false,
  nextBusy = false,
  nextAmount,
  className,
}: ProjectWorkflowFooterProps) {
  const { c } = useCatalogText();
  const tipText = tip?.trim();
  // 状态函数产出中文源文案，调用方可能直接透传；展示处兜底本地化。
  const tipCopy = tipText ? c(tipText) : '';
  const left =
    leftContent !== undefined && leftContent !== null ? (
      <div className="xy-dock-copy">{leftContent}</div>
    ) : tipText ? (
      <p
        className="xy-dock-copy"
        title={tipCopy}
        data-tone={tipTone}
        role="status"
        aria-live="polite"
      >
        {tipTone === 'success' ? (
          <Check aria-hidden />
        ) : tipTone === 'info' ? (
          <Spinner />
        ) : tipTone === 'warning' || tipTone === 'danger' ? (
          <CircleAlert aria-hidden />
        ) : (
          <Info aria-hidden />
        )}
        <span>{tipCopy}</span>
      </p>
    ) : null;
  return (
    <footer className={cn('xy-workflow-dock', className)}>
      <div className={cn('xy-dock-inner', left ? 'justify-between' : 'justify-end')}>
        {left}
        <div className="xy-dock-actions">
          {onPrevious ? (
            <Button
              variant="ghost"
              aria-label={c('上一步')}
              onClick={onPrevious}
              className="xy-dock-previous"
            >
              <ArrowLeft aria-hidden />
            </Button>
          ) : null}
          {onNext ? (
            <Button
              variant="accent"
              disabled={nextDisabled}
              busy={nextBusy}
              onClick={onNext}
              className="xy-dock-next"
            >
              {c('下一步')}
              {nextAmount !== undefined ? (
                <span className="button-credit-amount">
                  <CreditStar />
                  {formatInteger(String(nextAmount))}
                </span>
              ) : (
                <ArrowRight />
              )}
            </Button>
          ) : null}
        </div>
      </div>
    </footer>
  );
}
