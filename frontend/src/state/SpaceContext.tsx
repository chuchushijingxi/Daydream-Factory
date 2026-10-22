import {
  createContext,
  startTransition,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { useNavigate } from 'react-router-dom';
import { useSession } from './SessionContext';
import { spacesApi, type SpacesApi } from '../api/spaces';
import {
  spaceSelection,
  spaceStorageKey,
  spaceChannel,
  SpacePreferenceOrder,
  parseSpacePreference,
  persistSpacePreference,
  type SpacePreference,
} from '../api/space-selection';
import type { SpaceSummary } from '../api/spaces-types';
import { ApiError } from '../api/http-error';
import { toast } from '../components/ui/sonner';
import { translate } from '../contexts/translate';

// 切换提示每个账号对每个空间只出现一次：刷新恢复、其他标签页同步和之后的往返切换都不再重复。
// 浏览器存储不可用时无法判断是否首次，宁可照常提示。
export const spaceSwitchNoticeKey = (identity: string) => 'drama:space-switch-noticed:' + identity;
function firstSwitchTo(identity: string, spaceId: string) {
  try {
    const key = spaceSwitchNoticeKey(identity),
      seen: unknown = JSON.parse(localStorage.getItem(key) || '[]');
    const ids = Array.isArray(seen) ? seen.filter((id) => typeof id === 'string') : [];
    if (ids.includes(spaceId)) return false;
    localStorage.setItem(key, JSON.stringify([...ids, spaceId].slice(-100)));
    return true;
  } catch {
    return true;
  }
}

export type SpaceState = {
  spaceId: string;
  current: SpaceSummary | null;
  spaces: SpaceSummary[];
  epoch: number;
  ready: boolean;
  error: string;
  pendingInvites: number;
  refresh(): Promise<void>;
  // quiet：调用方自行提示失败（如团队直达链接），不再弹出原始错误。
  switchSpace(id: string, options?: { keepRoute?: boolean; quiet?: boolean }): Promise<boolean>;
};
const Context = createContext<SpaceState | null>(null);
export const useOptionalSpace = () => useContext(Context);
export function useSpace() {
  const value = useOptionalSpace();
  if (!value) throw new Error('SpaceProvider missing');
  return value;
}
export function SpaceProvider({
  children,
  service = spacesApi,
}: {
  children: ReactNode;
  service?: SpacesApi;
}) {
  const { user } = useSession();
  const identity = user?.identity || '';
  const navigate = useNavigate(),
    navigateRef = useRef(navigate);
  navigateRef.current = navigate;
  const [state, setState] = useState<Omit<SpaceState, 'refresh' | 'switchSpace'>>({
    spaceId: identity,
    current: null,
    spaces: [],
    epoch: spaceSelection.epoch,
    ready: false,
    error: '',
    pendingInvites: 0,
  });
  const latest = useRef(state),
    active = useRef(false),
    sequence = useRef(0),
    switching = useRef(0),
    pendingChoice = useRef(0),
    preference = useRef(new SpacePreferenceOrder()),
    committed = useRef<SpacePreference | null>(null),
    channel = useRef<BroadcastChannel | null>(null);
  latest.current = state;
  const update = (next: typeof state) => {
    latest.current = next;
    setState(next);
  };
  const commit = useCallback(
    (
      target: SpaceSummary,
      keepRoute: boolean,
      broadcast: boolean,
      rows = latest.current.spaces,
      incoming?: SpacePreference,
    ) => {
      if (incoming && !preference.current.isCurrent(incoming)) return;
      const record =
        incoming ||
        (preference.current.current?.spaceId === target.id
          ? preference.current.current
          : preference.current.select(target.id));
      committed.current = record;
      const changed = latest.current.spaceId !== target.id;
      // A late space-list refresh must not undo a more recent explicit switch.
      if (changed) ++sequence.current;
      spaceSelection.select(target.id);
      // HashRouter navigates in a transition. Commit the selected space in the
      // same transition, so an old team/project route cannot remount between
      // the new epoch and its default /projects navigation and select it back.
      startTransition(() => {
        if (!keepRoute) navigateRef.current('/projects');
        update({
          ...latest.current,
          spaceId: target.id,
          current: target,
          spaces: rows,
          epoch: spaceSelection.epoch,
          ready: true,
          error: '',
        });
      });
      try {
        persistSpacePreference(localStorage, spaceStorageKey(identity), record);
      } catch {
        /* Browser storage is optional. */
      }
      if (broadcast) channel.current?.postMessage(record);
      if (changed && firstSwitchTo(identity, target.id))
        toast.info(
          target.kind === 'personal'
            ? translate('已切换到个人空间')
            : translate('已切换到团队「{name}」', { name: target.name }),
          { id: 'space-selection' },
        );
    },
    [identity],
  );
  const choose = useCallback(
    async (
      id: string,
      options: { keepRoute?: boolean; quiet?: boolean } = {},
      incoming?: SpacePreference,
    ) => {
      const ticket = ++switching.current;
      pendingChoice.current = ticket;
      try {
        const detail = await service.detail(id);
        if (
          !active.current ||
          ticket !== switching.current ||
          (incoming && !preference.current.isCurrent(incoming))
        )
          return false;
        if (detail.status === 'disabled')
          throw new ApiError(404, 'NOT_FOUND', translate('无权限访问'));
        const rows = latest.current.spaces.some((row) => row.id === id)
          ? latest.current.spaces
          : [...latest.current.spaces, detail];
        // Observe durable versions before allocating a new local choice. A tab
        // may not have received the corresponding storage/BC event yet.
        if (!incoming) {
          try {
            preference.current.receive(localStorage.getItem(spaceStorageKey(identity)));
          } catch {
            /* Optional memory. */
          }
        }
        const record = incoming || preference.current.select(id);
        commit(detail, options.keepRoute === true, !incoming, rows, record);
        return true;
      } catch (error) {
        if (
          active.current &&
          ticket === switching.current &&
          !(error instanceof ApiError && error.code === 'SPACE_CHANGED')
        ) {
          // A remote choice can supersede the initial list request. If its
          // authorization fails, expose the existing retry UI immediately.
          if (!latest.current.ready) update({ ...latest.current, error: (error as Error).message });
          else if (!options.quiet) toast.error((error as Error).message);
        }
        return false;
      } finally {
        if (pendingChoice.current === ticket) pendingChoice.current = 0;
      }
    },
    [service, commit, identity],
  );
  const load = useCallback(
    async (restore?: string) => {
      const ticket = ++sequence.current;
      try {
        const [rows, invites] = await Promise.all([
          service.list(),
          service.pendingInvites().catch(() => null),
        ]);
        if (!active.current || ticket !== sequence.current || pendingChoice.current) return;
        const preferred = restore || latest.current.spaceId || identity;
        const target =
          rows.find((row) => row.id === preferred && row.status !== 'disabled') ||
          rows.find((row) => row.id === identity && row.kind === 'personal');
        if (!target)
          throw new ApiError(200, 'PROGRAM_FAILED', translate('个人空间读取失败，请重新读取。'));
        const fallback = target.id !== preferred;
        update({
          ...latest.current,
          spaces: rows.filter((row) => row.status !== 'disabled'),
          pendingInvites: invites?.length ?? latest.current.pendingInvites,
        });
        commit(
          target,
          !fallback,
          fallback,
          rows.filter((row) => row.status !== 'disabled'),
        );
        if (fallback)
          toast.warning(translate('原空间已不可访问，已返回个人空间。'), {
            id: 'space-selection',
          });
      } catch (error) {
        if (
          active.current &&
          ticket === sequence.current &&
          !(error instanceof ApiError && error.code === 'SPACE_CHANGED')
        )
          update({ ...latest.current, error: (error as Error).message });
      }
    },
    [identity, service, commit],
  );
  const refresh = useCallback(() => load(), [load]);
  const receive = useCallback(
    (value: unknown) => {
      let record = preference.current.receive(value);
      try {
        const durable = preference.current.receive(localStorage.getItem(spaceStorageKey(identity)));
        record = durable || record;
        if (!record && committed.current)
          persistSpacePreference(localStorage, spaceStorageKey(identity), committed.current);
      } catch {
        /* Optional memory. */
      }
      if (!record) return;
      ++sequence.current;
      void choose(record.spaceId, {}, record);
    },
    [choose, identity],
  );
  useEffect(() => {
    active.current = true;
    preference.current = new SpacePreferenceOrder();
    committed.current = null;
    pendingChoice.current = 0;
    spaceSelection.select(identity);
    let restored = identity;
    try {
      const stored = localStorage.getItem(spaceStorageKey(identity));
      const record = preference.current.receive(stored);
      // A previously remembered plain ID is only a lookup hint, never access.
      restored = record?.spaceId || (stored && !parseSpacePreference(stored) ? stored : identity);
    } catch {
      /* Optional memory. */
    }
    void load(restored);
    const connection =
      typeof BroadcastChannel === 'function' ? new BroadcastChannel(spaceChannel(identity)) : null;
    channel.current = connection;
    if (connection) connection.onmessage = (event) => receive(event.data);
    const stored = (event: StorageEvent) => {
      if (event.key === spaceStorageKey(identity)) receive(event.newValue);
    };
    window.addEventListener('storage', stored);
    const visible = () => {
      if (!document.hidden) void refresh();
    };
    window.addEventListener('focus', visible);
    document.addEventListener('visibilitychange', visible);
    const timer = window.setInterval(visible, 30000);
    return () => {
      active.current = false;
      ++sequence.current;
      ++switching.current;
      window.clearInterval(timer);
      window.removeEventListener('focus', visible);
      window.removeEventListener('storage', stored);
      document.removeEventListener('visibilitychange', visible);
      connection?.close();
      channel.current = null;
      spaceSelection.select('');
    };
  }, [identity, load, receive, refresh]);
  return (
    <Context.Provider value={{ ...state, refresh, switchSpace: choose }}>
      {children}
    </Context.Provider>
  );
}
