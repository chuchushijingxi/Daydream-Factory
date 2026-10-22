import type { LedgerView } from '../../api/administration';
import type { TeamCreditEntry } from '../../api/team-credits';
import type { CatalogText } from '../../contexts/useCatalogText';
import { formatInteger } from './credit-format';
export const creditDirectionLabels = {
  add: '获得',
  deduct: '扣减',
  refund: '返还',
  expire: '到期回收',
  reclaim: '退款扣回',
};
export function creditSignedAmount(entry: LedgerView) {
  const sign = ['deduct', 'expire', 'reclaim'].includes(entry.direction) ? '−' : '+';
  return sign + formatInteger(entry.amount);
}
const giftLabels = new Map([
  ['daily-gift', '每日赠送'],
  ['registration-gift', '注册赠送'],
  ['daily-gift-expiration', '每日赠送到期回收'],
]);
export function creditRecordTitle(entry: LedgerView, c: CatalogText) {
  if (entry.source.startsWith('payment-order:') &&
      ['充值到账', '充值赠送', '充值退款扣回', '支付退款失败恢复'].includes(entry.reason))
    return c(entry.reason);
  const label = giftLabels.get(entry.source);
  // 只翻译程序拥有的固定原因，运营手填原文不得被覆盖。
  if (label && (entry.reason === entry.source || entry.reason === label)) return c(label);
  if (entry.source !== 'operations-adjustment') {
    if (entry.reason === 'generation') return c('创作生成');
    if (entry.reason === 'generation-incomplete') return c('生成未完成返还');
  }
  return entry.reason || c(creditDirectionLabels[entry.direction] + '积分');
}
// 团队流水不带 source，只翻译程序拥有的生成原因；其余服务端原文照常显示。
export function teamCreditTitle(entry: TeamCreditEntry, c: CatalogText) {
  if (entry.reason === 'generation') return c('创作生成');
  if (entry.reason === 'generation-incomplete') return c('生成未完成返还');
  return entry.reason || c(creditDirectionLabels[entry.kind] + '积分');
}
export const allocationLabels = { allocate: '分配', reclaim: '收回', release: '退出收回' } as const;
export function creditSourceLabel(source: string, c: CatalogText) {
  if (source.startsWith('payment-order:')) return c('积分充值');
  const label = giftLabels.get(source) || (source === 'operations-adjustment' ? '运营调整' : '');
  return label ? c(label) : c('创作生成');
}
export function creditActorLabel(entry: LedgerView, c: CatalogText) {
  const paymentLabels: Record<string, string> = {
    'payment:demo': '演示支付',
    'payment:wechatpay': '微信支付',
    'payment:alipay': '支付宝',
  };
  if (entry.source.startsWith('payment-order:') && paymentLabels[entry.actorLabel])
    return c(paymentLabels[entry.actorLabel]);
  return entry.source === 'daily-gift-expiration' && entry.actorLabel === 'system'
    ? c('系统')
    : entry.actorLabel;
}
export function shortCreditTime(at: string) {
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(new Date(at));
}
export function creditCategoryLabel(value: LedgerView['category']) {
  return (
    {
      recharge: '充值积分',
      gift: '赠送及免费积分',
      legacy: '历史未分类积分',
      mixed: '混合积分',
    } as Record<string, string>
  )[value || 'legacy'];
}
