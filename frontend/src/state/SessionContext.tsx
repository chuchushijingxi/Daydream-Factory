import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { administration, type Administration, type SessionView } from '../api/administration';
import { ApiError } from '../api/http-error';
import {
  sessionOwnerStorageKey,
  clearAccountDrafts,
  sessionBoundary,
} from '../api/session-boundary';
import { toast } from '../components/ui/sonner';
import {
  sessionAudience,
  sessionChangeChannel,
  type SessionAudience,
} from '../api/session-audience';
import { SessionReplacementDialog } from '../pages/accounts/SessionReplacementDialog';
import { translate } from '../contexts/translate';

type SessionState = {
  state: 'checking' | 'anonymous' | 'authenticated' | 'unavailable';
  user: SessionView | null;
  message: string;
  epoch: number;
};
type SessionContextValue = SessionState & {
  service: Administration;
  refresh(): Promise<void>;
  authenticate(action: () => Promise<void>): Promise<boolean>;
  updateProfile(action: () => Promise<SessionView>): Promise<SessionView>;
  changePassword(current: string, password: string): Promise<void>;
  logout(): Promise<void>;
};
export const SessionOptionalContext = createContext<SessionContextValue | null>(null);
const Context = SessionOptionalContext;
export const useSession = () => {
  const value = useContext(Context);
  if (!value) throw new Error('SessionProvider missing');
  return value;
};
// 每个平台（audience）经 key 重挂载出唯一一个 SessionScope；session-boundary 的
// 模块级撤销钩子依赖"每平台同时只有一个 Scope"这一前提，新增挂载点前先核对。
export function SessionProvider({
  children,
  service = administration,
}: {
  children: ReactNode;
  service?: Administration;
}) {
  const [audience, setAudience] = useState(sessionAudience);
  const audienceRef = useRef(audience);
  useEffect(() => {
    const changed = () => {
      const next = sessionAudience();
      if (audienceRef.current !== next) {
        audienceRef.current = next;
        sessionBoundary.reset();
        setAudience(next);
      }
    };
    window.addEventListener('hashchange', changed);
    return () => window.removeEventListener('hashchange', changed);
  }, []);
  return (
    <SessionScope key={audience} audience={audience} service={service}>
      {children}
    </SessionScope>
  );
}
function SessionScope({
  children,
  service,
  audience,
}: {
  children: ReactNode;
  service: Administration;
  audience: SessionAudience;
}) {
  const [state, setState] = useState<SessionState>({
    state: 'checking',
    user: null,
    message: '',
    epoch: sessionBoundary.epoch,
  });
  const [replacement, setReplacement] = useState(false);
  const ended = useRef<'logout' | 'passwordChanged' | null>(null),
    active = useRef(true),
    recheckPending = useRef(false),
    replacementShown = useRef(false),
    sequence = useRef(0),
    mutation = useRef(false),
    stateRef = useRef(state);
  stateRef.current = state;
  const showReplacement = (error: ApiError) => {
    if (!ended.current && error.code === 'SESSION_REPLACED' && !replacementShown.current) {
      replacementShown.current = true;
      setReplacement(true);
    }
  };
  const loginNotice = (error?: ApiError) => {
    if (ended.current === 'passwordChanged')
      return translate('密码已修改，请使用新密码重新登录。');
    if (ended.current === 'logout' || !error) return '';
    // 旧Cookie的失效是匿名登录页的正常状态；替换原因由原专用弹窗提示。
    if (['AUTH_REQUIRED', 'SESSION_REVOKED', 'SESSION_EXPIRED', 'SESSION_REPLACED'].includes(error.code))
      return '';
    return service.sessionMessage(error.code) || error.message;
  };
  const ownerStorageKey = sessionOwnerStorageKey(audience);
  const clear = (resetBoundary = true) => {
    if (resetBoundary) sessionBoundary.reset();
    try {
      clearAccountDrafts(sessionStorage, audience);
    } catch {
      /* Browser storage can be unavailable. */
    }
    toast.dismiss();
  };
  const refresh = async () => {
    if (!active.current) return;
    const ticket = ++sequence.current,
      epoch = sessionBoundary.epoch;
    // Foreground rechecks run in the background; the initial state already
    // guards first load. Do not replace an in-progress authentication form.
    try {
      const user = await service.current();
      if (ticket !== sequence.current || epoch !== sessionBoundary.epoch) return;
      if (stateRef.current.user && stateRef.current.user.identity !== user?.identity) clear();
      // Cache ownership survives reloads and missed tab messages. It never proves login.
      try {
        if (sessionStorage.getItem(ownerStorageKey) !== user?.identity)
          clearAccountDrafts(sessionStorage, audience);
        if (user) sessionStorage.setItem(ownerStorageKey, user.identity);
        else sessionStorage.removeItem(ownerStorageKey);
      } catch {
        /* Storage is optional; the authenticated server response remains authoritative. */
      }
      if (user) {
        ended.current = null;
        replacementShown.current = false;
        setReplacement(false);
      }
      setState({
        state: user ? 'authenticated' : 'anonymous',
        user,
        message: user ? '' : loginNotice(),
        epoch: sessionBoundary.epoch,
      });
    } catch (error) {
      if (ticket !== sequence.current || epoch !== sessionBoundary.epoch) return;
      if (error instanceof ApiError && error.status === 401) {
        showReplacement(error);
        clear(stateRef.current.user !== null);
        setState({
          state: 'anonymous',
          user: null,
          message: loginNotice(error),
          epoch: sessionBoundary.epoch,
        });
      } else
        setState({ state: 'unavailable', user: null, message: (error as Error).message, epoch });
    }
  };
  const finishMutation = () => {
    mutation.current = false;
    if (active.current && recheckPending.current) {
      recheckPending.current = false;
      void refresh();
    }
  };
  useEffect(() => {
    active.current = true;
    sessionBoundary.onUnauthorized = (error) => {
      ++sequence.current;
      showReplacement(error);
      try {
        clearAccountDrafts(sessionStorage, audience);
      } catch {
        /* No persistent drafts available. */
      }
      toast.dismiss();
      setState({
        state: 'anonymous',
        user: null,
        message: loginNotice(error),
        epoch: sessionBoundary.epoch,
      });
    };
    void refresh();
    const channel =
      typeof BroadcastChannel === 'function'
        ? new BroadcastChannel(sessionChangeChannel(audience))
        : null;
    if (channel)
      channel.onmessage = (event) => {
        if (event.data !== 'changed') return;
        if (mutation.current) {
          recheckPending.current = true;
          return;
        }
        clear();
        void refresh();
      };
    const recheck = () => {
      if (document.visibilityState === 'visible' && !mutation.current) void refresh();
    };
    window.addEventListener('focus', recheck);
    window.addEventListener('pageshow', recheck);
    document.addEventListener('visibilitychange', recheck);
    // Idle foreground operations pages must also notice revocation/replacement.
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      if (
        stateRef.current.state === 'authenticated' &&
        !mutation.current &&
        document.visibilityState === 'visible'
      )
        await refresh();
      if (!stopped) timer = setTimeout(poll, 30000);
    };
    timer = setTimeout(poll, 30000);
    return () => {
      active.current = false;
      stopped = true;
      clearTimeout(timer);
      ++sequence.current;
      window.removeEventListener('focus', recheck);
      window.removeEventListener('pageshow', recheck);
      document.removeEventListener('visibilitychange', recheck);
      channel?.close();
      sessionBoundary.onUnauthorized = undefined;
    };
  }, [service, audience]);
  const notify = () => {
    if (typeof BroadcastChannel === 'function') {
      const channel = new BroadcastChannel(sessionChangeChannel(audience));
      channel.postMessage('changed');
      channel.close();
    }
  };
  return (
    <Context.Provider
      value={{
        ...state,
        service,
        refresh,
        async authenticate(action) {
          if (mutation.current) return false;
          mutation.current = true;
          const epoch = sessionBoundary.epoch;
          try {
            await action();
            sessionBoundary.assert(epoch);
            clear();
            notify();
            await refresh();
            return true;
          } finally {
            finishMutation();
          }
        },
        async updateProfile(action) {
          if (mutation.current) throw new Error(translate('正在提交，请稍候。'));
          mutation.current = true;
          ++sequence.current;
          const epoch = sessionBoundary.epoch,
            id = stateRef.current.user?.identity;
          try {
            const user = await action();
            sessionBoundary.assert(epoch);
            if (!id || user.identity !== id)
              throw new Error(translate('账号已变更，请重新打开个人资料。'));
            ++sequence.current;
            setState({ state: 'authenticated', user, message: '', epoch });
            return user;
          } catch (error) {
            if (
              epoch === sessionBoundary.epoch &&
              error instanceof ApiError &&
              error.status === 401
            )
              sessionBoundary.unauthorized(epoch, error);
            throw error;
          } finally {
            finishMutation();
          }
        },
        async changePassword(current, password) {
          if (mutation.current) throw new Error(translate('正在提交，请稍候。'));
          mutation.current = true;
          ++sequence.current;
          const epoch = sessionBoundary.epoch;
          try {
            await service.changePassword(current, password);
            sessionBoundary.assert(epoch);
            ended.current = 'passwordChanged';
            clear();
            notify();
            setState({
              state: 'anonymous',
              user: null,
              message: loginNotice(),
              epoch: sessionBoundary.epoch,
            });
          } finally {
            finishMutation();
          }
        },
        async logout() {
          if (mutation.current) return;
          mutation.current = true;
          ++sequence.current;
          clear();
          const epoch = sessionBoundary.epoch;
          // Clear visible private data immediately. Keep login serialized until the Cookie response settles.
          setState({ state: 'checking', user: null, message: translate('正在退出…'), epoch });
          try {
            await service.logout();
            sessionBoundary.assert(epoch);
            ended.current = 'logout';
            clear();
            notify();
            setState({
              state: 'anonymous',
              user: null,
              message: '',
              epoch: sessionBoundary.epoch,
            });
          } catch (error) {
            if (epoch === sessionBoundary.epoch) {
              clear();
              setState({
                state: 'unavailable',
                user: null,
                message:
                  error instanceof ApiError
                    ? error.message
                    : translate('退出结果未确认，请重新核对会话。'),
                epoch: sessionBoundary.epoch,
              });
            }
          } finally {
            finishMutation();
          }
        },
      }}
    >
      {children}
      <SessionReplacementDialog
        open={replacement}
        audience={audience}
        onOpenChange={setReplacement}
      />
    </Context.Provider>
  );
}
