import type { ModelOption } from '../../api/types';
import { useContext } from 'react';
import { SessionOptionalContext } from '../../state/SessionContext';
import type { ChargeQuoteDTO } from '../../api/administration-types';
import { useRead } from '../../pages/operations/controls';
import { formatInteger } from '../../pages/credits/credit-format';
import { CreditStar } from '../../components/ui/credit-star';
import { FreeBadge } from '../../components/ui/free-badge';
import { Button } from '../../components/ui/button';
import { useCatalogText } from '../../contexts/useCatalogText';
export function useChargeQuote(load: () => Promise<ChargeQuoteDTO>, key: string, enabled: boolean) {
  const session = useContext(SessionOptionalContext);
  // All production starts live under SessionProvider. Component-only free tests
  // may omit it; they exercise the original content contract without billing.
  const required = !!session?.user;
  const read = useRead(load, [key, required, session?.epoch], required && enabled);
  return {
    ...read,
    required,
    basis: required && read.value ? read.value.basis : undefined,
    blocked: required && (!enabled || read.busy || !!read.error || !read.value),
  };
}
export function ChargeNotice({ quote }: { quote: ReturnType<typeof useChargeQuote> }) {
  const { c } = useCatalogText();
  if (!quote.required) return null;
  if (quote.error)
    return (
      <span className="charge-notice op-inline-error" role="alert">
        {quote.error}
        <Button size="sm" variant="link" onClick={quote.refresh}>
          {c('重新读取价格')}
        </Button>
      </span>
    );
  return null;
}
// 限免报价不显示积分；标签已表达本次不扣费。
export function QuoteAmount({ quote }: { quote: ReturnType<typeof useChargeQuote> }) {
  if (!quote.value) return null;
  if (quote.value.free) return <FreeBadge />;
  return (
    <span className="button-credit-amount">
      <CreditStar />
      {formatInteger(String(quote.value.amount))}
    </span>
  );
}
export function ModelPriceLabel({
  model,
  category,
}: {
  model: ModelOption;
  category: 'llmModel' | 'imageModel' | 'videoModel';
}) {
  const { c } = useCatalogText();
  const rule = model.creditPrice;
  const unitSuffix =
    model.billingUnit === 'characters-100'
      ? c('积分/100 字')
      : model.billingUnit === 'image'
        ? c('积分/张')
        : model.billingUnit === 'second'
          ? c('积分/秒')
          : null;
  if (!rule || !unitSuffix)
    return (
      <small className="xy-model-price">
        {c('{category}积分价格待确认', {
          category: c(category === 'llmModel' ? '语言' : category === 'imageModel' ? '图片' : '视频'),
        })}
      </small>
    );
  if (model.free === true) return <FreeBadge />;
  const rates = [rule.unitPrice, ...Object.values(rule.resolutionPrices || {})];
  const min = Math.min(...rates),
    max = Math.max(...rates);
  return (
    <small className="xy-model-price">
      {formatInteger(String(min))}
      {max > min ? '–' + formatInteger(String(max)) : ''} {unitSuffix}
      {model.mediaProfile && model.mediaProfile.resolutions.length > 1
        ? ' · ' + c('按分辨率')
        : ''}
    </small>
  );
}
