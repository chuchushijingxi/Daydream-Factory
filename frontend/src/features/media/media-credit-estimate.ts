import type { MediaOptions, ModelOption } from '../../api/types';

// 显示复用已加载模型目录的档位价格；提交仍使用服务端报价。
export function mediaCreditEstimate(
  model: ModelOption | undefined,
  options: MediaOptions | undefined,
  quantity = 1,
): number | undefined {
  if (!model || !options || !Number.isSafeInteger(quantity) || quantity <= 0) return;
  const price = model.creditPrice;
  const amount =
    price?.resolutionPrices?.[options.resolution] ??
    (options.resolution === model.mediaProfile?.defaultResolution ? price?.unitPrice : undefined);
  if (typeof amount !== 'number' || !Number.isSafeInteger(amount) || amount < 0) return;
  const total = amount * quantity;
  if (!Number.isSafeInteger(total)) return;
  return model.free ? 0 : total;
}
