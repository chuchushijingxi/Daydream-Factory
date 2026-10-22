import { ApiError } from './http-error';
import { translate } from '../contexts/translate';

// Browser selection and generation only. The header is never authorization.
export class SpaceSelection {
  private selected = '';
  private generation = 0;
  private requests = new Set<AbortController>();
  private listeners = new Set<() => void>();
  get spaceId() {
    return this.selected;
  }
  get epoch() {
    return this.generation;
  }
  select(id: string) {
    if (this.selected === id) return;
    this.selected = id;
    this.generation++;
    for (const request of this.requests) request.abort();
    this.requests.clear();
    for (const listener of this.listeners) listener();
  }
  assert(epoch: number) {
    if (epoch !== this.generation)
      throw new ApiError(0, 'SPACE_CHANGED', translate('空间已变化，未采用原空间的请求结果。'));
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
}
export const spaceSelection = new SpaceSelection();
export const spaceStorageKey = (identity: string) => 'drama:space:' + identity;
export const spaceChannel = (identity: string) => 'space:' + identity;

// A Lamport counter plus a stable tab ID orders simultaneous tab choices. The
// exact record is used for both storage and BroadcastChannel; neither is authority.
export type SpacePreference = { spaceId: string; counter: number; tabId: string };
export function parseSpacePreference(value: unknown): SpacePreference | null {
  if (typeof value === 'string') {
    try {
      value = JSON.parse(value);
    } catch {
      return null;
    }
  }
  if (!value || typeof value !== 'object') return null;
  const row = value as Record<string, unknown>;
  if (
    typeof row.spaceId !== 'string' ||
    !row.spaceId ||
    !Number.isSafeInteger(row.counter) ||
    (row.counter as number) < 1 ||
    (row.counter as number) >= Number.MAX_SAFE_INTEGER ||
    typeof row.tabId !== 'string' ||
    !row.tabId
  )
    return null;
  return { spaceId: row.spaceId, counter: row.counter as number, tabId: row.tabId };
}
export function compareSpacePreferences(left: SpacePreference, right: SpacePreference): number {
  return (
    left.counter - right.counter ||
    (left.tabId < right.tabId ? -1 : left.tabId > right.tabId ? 1 : 0) ||
    (left.spaceId < right.spaceId ? -1 : left.spaceId > right.spaceId ? 1 : 0)
  );
}
const tabId = globalThis.crypto?.randomUUID?.() || Date.now() + ':' + Math.random();
export class SpacePreferenceOrder {
  current: SpacePreference | null = null;
  constructor(private readonly source = tabId) {}
  receive(value: unknown): SpacePreference | null {
    const record = parseSpacePreference(value);
    if (!record || (this.current && compareSpacePreferences(record, this.current) <= 0))
      return null;
    this.current = record;
    return record;
  }
  select(spaceId: string): SpacePreference {
    const record = { spaceId, counter: (this.current?.counter || 0) + 1, tabId: this.source };
    this.current = record;
    return record;
  }
  isCurrent(record: SpacePreference): boolean {
    return this.current !== null && compareSpacePreferences(this.current, record) === 0;
  }
}
// Storage writes can race across processes. Receiving an older record repairs
// the shared value to the same winner, without rebroadcasting or reauthorizing it.
export function persistSpacePreference(
  storage: Pick<Storage, 'getItem' | 'setItem'>,
  key: string,
  record: SpacePreference,
) {
  const stored = parseSpacePreference(storage.getItem(key));
  if (!stored || compareSpacePreferences(stored, record) < 0)
    storage.setItem(key, JSON.stringify(record));
}

// Only same-application paths can be restored after login; never an external URL.
export function safeReturnTo(value: string | null): string {
  return value &&
    value.startsWith('/') &&
    !value.startsWith('//') &&
    !/[\\\u0000-\u001f]/.test(value)
    ? value
    : '';
}
