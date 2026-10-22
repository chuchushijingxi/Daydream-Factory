import { useOptionalSpace } from '../../state/SpaceContext';
import { holdsCredits, teamCreditsApi, type TeamBalance } from '../../api/team-credits';
import { createContext, useContext, useEffect, type ReactNode } from 'react';
import { SessionOptionalContext } from '../../state/SessionContext';
import type { CreditBalanceView } from '../../api/administration';
import { useCreditRead } from './useCreditRead';
import { useCreditUpdates } from './useCreditUpdates';

type BalanceState = {
  wallet: CreditBalanceView | null;
  balance: string | null;
  busy: boolean;
  error: string;
  syncError: string;
  revision: number;
  enabled: boolean;
  refresh: () => void;
};
const Context = createContext<BalanceState | null>(null);
// Presentation state only. Both surfaces read the same current-account balance;
// the server still owns the account, integer arithmetic and ledger.
export function PersonalCreditsProvider({ children }: { children: ReactNode }) {
  const space = useOptionalSpace();
  return space?.current?.kind === 'team' ? (
    <TeamCreditsProvider>{children}</TeamCreditsProvider>
  ) : (
    <PersonalCreditsContent>{children}</PersonalCreditsContent>
  );
}
function PersonalCreditsContent({ children }: { children: ReactNode }) {
  const session = useContext(SessionOptionalContext),
    enabled = !!session?.user;
  const read = useCreditRead(
    () => session!.service.myCreditBalance(),
    [session?.service, session?.user?.identity],
    enabled,
  );
  const updates = useCreditUpdates(enabled, null, read.refresh);
  useEffect(() => {
    if (!enabled) return;
    const refresh = () => {
      if (document.visibilityState === 'visible') read.refresh();
    };
    window.addEventListener('focus', refresh);
    return () => window.removeEventListener('focus', refresh);
  }, [session?.user?.identity, enabled]);
  return (
    <Context.Provider
      value={{
        wallet: read.value,
        balance: read.value?.balance ?? null,
        busy: read.busy,
        error: read.error,
        syncError: updates.error,
        revision: updates.revision,
        enabled,
        refresh: read.refresh,
      }}
    >
      {children}
    </Context.Provider>
  );
}
export function usePersonalCredits() {
  return useContext(Context);
}

type TeamBalanceState = {
  wallet: TeamBalance | null;
  balance: string | null;
  busy: boolean;
  error: string;
  syncError: string;
  revision: number;
  refresh: () => void;
};
const TeamContext = createContext<TeamBalanceState | null>(null);
function TeamCreditsProvider({ children }: { children: ReactNode }) {
  const space = useOptionalSpace();
  const read = useCreditRead(
    () => teamCreditsApi.balance(),
    [space?.spaceId, space?.epoch],
    Boolean(space?.ready),
  );
  const updates = useCreditUpdates(Boolean(space?.ready), null, read.refresh);
  // 管理操作回读空间后立即核余额；SSE仍负责其他页面/节点的后续变化。
  useEffect(() => {
    if (space?.ready) read.refresh();
  }, [space?.current]);
  useEffect(() => {
    const visible = () => {
      if (!document.hidden) read.refresh();
    };
    window.addEventListener('focus', visible);
    return () => window.removeEventListener('focus', visible);
  }, [space?.spaceId]);
  // 空间角色变化后先隐藏旧角色投影，等服务端返回新身份可见的余额。
  const wallet =
    read.value?.spaceId === space?.spaceId && read.value?.role === space?.current?.role
      ? read.value
      : null;
  return (
    <TeamContext.Provider
      value={{
        wallet,
        // 所有者顶栏显示未分配积分；成员与管理员显示本人名下分配额。
        balance: holdsCredits(wallet?.role) ? (wallet?.balance ?? null) : (wallet?.unallocated ?? null),
        busy: read.busy,
        error: read.error,
        syncError: updates.error,
        revision: updates.revision,
        refresh: read.refresh,
      }}
    >
      {children}
    </TeamContext.Provider>
  );
}
export function useTeamCredits() {
  return useContext(TeamContext);
}
