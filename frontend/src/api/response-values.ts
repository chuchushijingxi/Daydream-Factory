import { ApiError } from './http-error';
import { translate } from '../contexts/translate';
import type { PageDTO } from './administration-types';
import type { PageView } from './administration';

// 响应值转换不依赖任何API实例，积分与支付可作为独立入口加载。
const malformed = () => new ApiError(200, 'PROGRAM_FAILED', translate('服务端回执与模块合同不一致，未采用响应。'));
export function safeInteger(value: number): string {
  if (!Number.isSafeInteger(value) || value < 0) throw malformed();
  return String(value);
}
export function creditInput(value: string): number {
  if (!/^[1-9][0-9]*$/.test(value) || BigInt(value) > 9007199254740991n)
    throw new ApiError(
      400,
      'CREDIT_INVALID_AMOUNT',
      translate('积分须为 1 至 9,007,199,254,740,991 的整数。'),
    );
  return Number(value);
}
export function pageView<T, U>(v: PageDTO<T>, map: (item: T) => U): PageView<U> {
  if (
    !Array.isArray(v.items) ||
    !Number.isSafeInteger(v.page) ||
    v.page < 1 ||
    !Number.isSafeInteger(v.pageSize) ||
    v.pageSize < 1 ||
    v.pageSize > 100
  )
    throw malformed();
  const total = safeInteger(v.total);
  return {
    items: v.items.map(map),
    previous: v.page > 1 ? String(v.page - 1) : null,
    next: BigInt(v.page) * BigInt(v.pageSize) < BigInt(total) ? String(v.page + 1) : null,
    caption: translate('共 {total} 条 · 第 {page} 页', {
      total: BigInt(total).toLocaleString('zh-CN'),
      page: v.page,
    }),
  };
}
