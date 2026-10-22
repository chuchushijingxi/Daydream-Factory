import { useCatalogText } from '../../contexts/useCatalogText';
import { formatPaymentAmount } from '../../domain/payment-format';

// 页面只传入档位或订单自身的冻结双价，不回查当前配置拼接历史优惠。
export function PaymentPrice({ amountFen, originalAmountFen }: {
  amountFen: number;
  originalAmountFen: number;
}) {
  const { c } = useCatalogText();
  const amount = formatPaymentAmount(amountFen);
  return (
    <span className={'payment-price' + (amount.length > 12 ? ' payment-price-long' : '')}>
      <strong aria-label={c('实付金额 {amount}', { amount })}>{amount}</strong>
      {originalAmountFen > amountFen ? (
        <del aria-label={c('原价 {amount}', { amount: formatPaymentAmount(originalAmountFen) })}>
          {formatPaymentAmount(originalAmountFen)}
        </del>
      ) : null}
    </span>
  );
}
