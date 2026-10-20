import { ApiError } from './http-error';
import { translate } from '../contexts/translate';

// 读取允许增加元数据；只让当前消费者声明的字段进入页面模型。
export function responseFields<T extends object, K extends keyof T>(
  value: T,
  keys: readonly K[],
): Pick<T, K> {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new ApiError(200, 'PROGRAM_FAILED', translate('接口响应不完整，请重新读取。'));
  return Object.fromEntries(
    keys.filter((key) => Object.hasOwn(value, key)).map((key) => [key, value[key]]),
  ) as Pick<T, K>;
}
