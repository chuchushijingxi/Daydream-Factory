import { http, ApiError } from './client';
import { sessionBoundary } from './session-boundary';
import { translate } from '../contexts/translate';
// Downloads retain binary parsing while sharing session errors and late-body isolation.
export async function downloadProtected(url: string, filename: string) {
  if (!url.startsWith('/api/') || url.includes('?') || url.includes('#'))
    throw new ApiError(200, 'PROGRAM_FAILED', translate('下载地址不符合受保护同源接口合同。'));
  const epoch = sessionBoundary.epoch;
  const blob = await http.binary(url, undefined, (response) => response.blob());
  sessionBoundary.assert(epoch);
  const objectUrl = URL.createObjectURL(blob),
    link = document.createElement('a');
  const revoke = () => {
    URL.revokeObjectURL(objectUrl);
    unsubscribe();
  };
  const unsubscribe = sessionBoundary.subscribe(revoke);
  link.href = objectUrl;
  link.download = filename;
  link.click();
  setTimeout(revoke, 1000);
}
