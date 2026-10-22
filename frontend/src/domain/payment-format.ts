import type { PaymentChannelID, PaymentStatus, PaymentTarget } from '../api/payment-types';
import { ApiError } from '../api/http-error';
import type { CatalogText } from '../contexts/useCatalogText';
import { translate } from '../contexts/translate';

const invalid = () => new ApiError(400, 'PAYMENT_INVALID_AMOUNT', translate('请输入安全范围内的有效金额或积分。'));
function amount(value: number) {
  if (!Number.isSafeInteger(value) || value < 0) throw invalid();
  return BigInt(value);
}
export function formatPaymentAmount(fen: number) {
  const n = amount(fen);
  return '¥' + String(n / 100n) + '.' + String(n % 100n).padStart(2, '0');
}
// 表单输入按十进制文本拼接分，不经parseFloat或乘100，避免大额尾分被舍入。
export function paymentAmountInput(yuan: string): number {
  if (typeof yuan !== 'string') throw invalid();
  const parts = /^(0|[1-9][0-9]*)(?:\.([0-9]{1,2}))?$/.exec(yuan.trim());
  if (!parts) throw invalid();
  const n = BigInt(parts[1]) * 100n + BigInt((parts[2] || '').padEnd(2, '0'));
  if (n > BigInt(Number.MAX_SAFE_INTEGER)) throw invalid();
  return Number(n);
}
export function paymentCreditsInput(text: string, allowZero = false): number {
  if (typeof text !== 'string' || !/^(0|[1-9][0-9]*)$/.test(text.trim())) throw invalid();
  const n = BigInt(text.trim());
  if (n > BigInt(Number.MAX_SAFE_INTEGER) || (!allowZero && n === 0n)) throw invalid();
  return Number(n);
}
export function paymentChannelLabel(channel: PaymentChannelID, c: CatalogText) {
  return c({ demo: '演示支付', wechatpay: '微信支付', alipay: '支付宝' }[channel]);
}
export function paymentStatusLabel(status: PaymentStatus, c: CatalogText) {
  return c({
    pending: '待支付',
    credited: '已到账',
    closed: '订单已关闭',
    refunded: '已退款'
  }[status]);
}
export function paymentTargetLabel(target: PaymentTarget, c: CatalogText) {
  return target.kind === 'personal' ? c('个人空间') : target.name;
}
