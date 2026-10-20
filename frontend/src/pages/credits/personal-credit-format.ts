import type { LedgerView } from '../../api/administration';
import type { CatalogText } from '../../contexts/useCatalogText';
import { formatInteger } from './credit-format';
export const creditDirectionLabels = {
  add: '获得',
  deduct: '扣减',
  refund: '返还',
  expire: '到期回收',
};
export function creditSignedAmount(entry: LedgerView) {
  return (['deduct', 'expire'].includes(entry.direction) ? '−' : '+') + formatInteger(entry.amount);
}
const giftLabels = new Map([
  ['daily-gift', '每日赠送'],
  ['registration-gift', '注册赠送'],
  ['daily-gift-expiration', '每日赠送到期回收'],
]);
export function creditRecordTitle(entry: LedgerView, c: CatalogText) {
  const label = giftLabels.get(entry.source);
  // 只翻译程序拥有的固定原因，运营手填原文不得被覆盖。
  if (label && (entry.reason === entry.source || entry.reason === label)) return c(label);
  if (entry.source !== 'operations-adjustment') {
    if (entry.reason === 'generation') return c('创作生成');
    if (entry.reason === 'generation-incomplete') return c('生成未完成返还');
  }
  return entry.reason || c(creditDirectionLabels[entry.direction] + '积分');
}
export function creditSourceLabel(source: string, c: CatalogText) {
  const label = giftLabels.get(source) || (source === 'operations-adjustment' ? '运营调整' : '');
  return label ? c(label) : c('创作生成');
}
export function creditActorLabel(entry: LedgerView, c: CatalogText) {
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
