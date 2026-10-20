import { ApiError } from './http-error';
import type { SessionAudience } from './session-audience';
import { translate } from '../contexts/translate';

// Browser-local generation only. Never a credential, Actor, role, or login proof.
export class SessionBoundary {
  private generation = 0;
  private listeners = new Set<() => void>();
  private requests = new Set<AbortController>();
  onUnauthorized: ((error: ApiError) => void) | undefined;
  get epoch() {
    return this.generation;
  }
  assert(epoch: number) {
    if (epoch !== this.generation)
      throw new ApiError(0, 'SESSION_CHANGED', translate('会话已变化，未采用旧请求结果。'));
  }
  track(controller: AbortController) {
    this.requests.add(controller);
    return () => {
      this.requests.delete(controller);
    };
  }
  subscribe(listener: () => void) {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }
  reset() {
    this.generation++;
    for (const controller of this.requests) controller.abort();
    this.requests.clear();
    for (const listener of this.listeners) listener();
  }
  unauthorized(epoch: number, error: ApiError) {
    this.assert(epoch);
    this.reset();
    this.onUnauthorized?.(error);
  }
}
export const sessionBoundary = new SessionBoundary();
export const accountOwnerStorageKey = 'drama:session-owner';
export const sessionOwnerStorageKey = (audience: SessionAudience) =>
  accountOwnerStorageKey + ':' + audience;

export function clearAccountDrafts(storage: Storage, audience?: SessionAudience) {
  // Only this application's account data; theme/language and other projects survive.
  const prefixes =
    audience === 'workspace'
      ? ['media-command:', 'image-batch:']
      : audience === 'operations'
        ? ['drama:mock-command:', 'drama:credit-command:']
        : ['media-command:', 'image-batch:', 'drama:mock-command:', 'drama:credit-command:'];
  for (let i = storage.length - 1; i >= 0; i--) {
    const key = storage.key(i);
    if (
      key &&
      (key === accountOwnerStorageKey ||
        (audience
          ? key === sessionOwnerStorageKey(audience)
          : key.startsWith(accountOwnerStorageKey + ':')) ||
        prefixes.some((prefix) => key.startsWith(prefix)))
    )
      storage.removeItem(key);
  }
}
