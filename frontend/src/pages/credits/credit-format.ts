import type { ChargeView } from '../../api/administration';
import type { CatalogText } from '../../contexts/useCatalogText';
import { translate } from '../../contexts/translate';
// Pure helpers keep the zh source text when no translator is supplied (unit tests,
// non-component callers). UI call sites pass the current c() to localize.
const sourceText: CatalogText = (text, values = {}) =>
  Object.entries(values).reduce(
    (out, [key, value]) => out.split('{' + key + '}').join(String(value)),
    text,
  );
// Integers remain decimal text at the presentation boundary, including int64 values.
// The production adapter must reject unsafe numeric JSON before mapping to these views.
export function integer(value: string, positive = false): string {
  if (!/^(0|[1-9][0-9]*)$/.test(value) || (positive && value === '0'))
    throw new Error(translate(positive ? '请输入有效的正整数积分。' : '请输入有效的非负整数积分。'));
  return value;
}
export function formatInteger(value: string): string {
  return BigInt(integer(value)).toLocaleString('zh-CN');
}
export function formatCost(value: string | null, c?: CatalogText): string {
  const t = c || sourceText;
  if (value === null) return t('待确认');
  if (!/^(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(value))
    throw new Error(translate('平台成本回执无效。'));
  const [whole, fraction] = value.split('.');
  return '¥' + formatInteger(whole) + (fraction === undefined ? '' : '.' + fraction);
}
export const unitLabel = (unit: ChargeView['unit'], c?: CatalogText) => {
  const t = c || sourceText;
  return unit === 'characters' ? t('字') : unit === 'images' ? t('张') : t('秒');
};
export function chargeFormula(charge: ChargeView, c?: CatalogText) {
  const t = c || sourceText;
  const priceSuffix =
    charge.unit === 'characters'
      ? t('积分/100 字')
      : charge.unit === 'images'
        ? t('积分/张')
        : t('积分/秒');
  return t('{quantity} {unit} × {price} {priceSuffix}＝{original} 积分', {
    quantity: formatInteger(charge.quantity),
    unit: unitLabel(charge.unit, c),
    price: formatInteger(charge.unitPrice),
    priceSuffix,
    original: formatInteger(charge.original),
  });
}
export function dateTime(value: string | null) {
  return value
    ? new Intl.DateTimeFormat('zh-CN', {
        timeZone: 'Asia/Shanghai',
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false,
      }).format(new Date(value))
    : '—';
}
export function shanghaiInput(value: string): string {
  if (!value) return '';
  const date = new Date(value + ':00+08:00');
  if (!Number.isFinite(date.getTime())) throw new Error(translate('请选择有效的日期时间。'));
  return date.toISOString();
}
