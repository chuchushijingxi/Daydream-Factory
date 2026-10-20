import { protectedResourceUrl } from '../../api/client';

// 旧项目快照沿用同一预览路径；资源换图后也必须退出浏览器里缓存的旧项目示例。
export function stylePreviewUrl(previewUrl: string) {
  const url = protectedResourceUrl(previewUrl);
  return url.startsWith('/demo/assets/styles/')
    ? `${url}${url.includes('?') ? '&' : '?'}v=daydream-20261019`
    : url;
}
