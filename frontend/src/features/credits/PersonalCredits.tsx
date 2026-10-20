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
