import { ApiError, type createHttpClient } from './client';
import { translate } from '../contexts/translate';

// Share only invalidation signals across same-origin views. The elected reader
// still authenticates through the original HTTP transport; followers reread
// their own protected resources. No content, credentials or session errors cross
// this channel. Releasing the Web Lock lets another live view take over.
export async function shareInvalidations(
  account: string,
  scope: string,
  connect: (changed: () => void, signal: AbortSignal) => Promise<void>,
  onChange: () => void,
  signal: AbortSignal,
) {
  if (
    typeof navigator === 'undefined' ||
    !navigator.locks ||
    typeof BroadcastChannel === 'undefined'
  )
    return connect(onChange, signal);
  if (signal.aborted) return;
  const key = 'drama:invalidation:' + JSON.stringify([account, scope]);
  const channel = new BroadcastChannel(key);
  const receive = (event: MessageEvent) => {
    const data = event.data;
    if (
      !signal.aborted &&
      data &&
      typeof data === 'object' &&
      !Array.isArray(data) &&
      Object.keys(data).length === 1 &&
      data.type === 'invalidate'
    )
      onChange();
  };
  channel.addEventListener('message', receive);
  try {
    // Covers a follower joining after the leader's ready frame.
    onChange();
    await navigator.locks.request(key, { signal }, async () => {
      if (signal.aborted) return;
      await connect(() => {
        if (signal.aborted) return;
        onChange();
        channel.postMessage({ type: 'invalidate' });
      }, signal);
    });
  } finally {
    channel.removeEventListener('message', receive);
    channel.close();
  }
}

// Business streams carry only invalidation signals. Cookie/session fencing and
// binary chunk transport stay in the one HTTP client.
export async function watchInvalidations(
  transport: ReturnType<typeof createHttpClient>,
  url: string,
  prefix: string,
  onChange: () => void,
  signal: AbortSignal,
  closedCode = 'STREAM_CLOSED',
) {
  const decoder = new TextDecoder();
  let buffer = '';
  await transport.stream(url, { signal, headers: { Accept: 'text/event-stream' } }, (chunk) => {
    buffer += decoder.decode(chunk, { stream: true });
    for (;;) {
      const boundary = /\r?\n\r?\n/.exec(buffer);
      if (!boundary) break;
      const frame = buffer.slice(0, boundary.index);
      buffer = buffer.slice(boundary.index + boundary[0].length);
      if (frame.length > 8192)
        throw new ApiError(200, 'PROGRAM_FAILED', translate('实时同步帧超出合同范围。'));
      let event = '',
        data = '';
      for (const line of frame.split(/\r?\n/)) {
        if (line.startsWith('event:')) event = line.slice(6).trim();
        else if (line.startsWith('data:')) data += line.slice(5).trim();
      }
      if (!event) continue;
      if (![prefix + '.ready', prefix + '.changed'].includes(event) || data !== '{}')
        throw new ApiError(
          200,
          'PROGRAM_FAILED',
          translate('实时同步包含合同外数据，未采用事件。'),
        );
      onChange();
    }
    if (buffer.length > 8192) throw new ApiError(200, 'PROGRAM_FAILED', translate('实时同步帧超出合同范围。'));
  });
  throw new ApiError(0, closedCode, translate('实时同步已断开，正在重新连接。'));
}
