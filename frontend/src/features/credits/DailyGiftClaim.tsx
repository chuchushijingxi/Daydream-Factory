import { useOptionalSpace } from '../../state/SpaceContext';
import { useCallback, useContext, useEffect, useRef, useState } from 'react';
import { SessionOptionalContext, useSession } from '../../state/SessionContext';
import { sessionBoundary } from '../../api/session-boundary';
import { usePersonalCredits } from './PersonalCredits';
import { Button } from '../../components/ui/button';
import { toast } from '../../components/ui/sonner';
import { formatInteger } from '../../pages/credits/credit-format';
import { useCatalogText } from '../../contexts/useCatalogText';
import { translate } from '../../contexts/translate';
function shanghaiDate() {
  return new Intl.DateTimeFormat('sv-SE', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(new Date());
}
// A separate workspace command. Authentication and read-only balance requests
// never award credits; the server's day receipt makes retries safe.
export function DailyGiftClaim() {
  const session = useContext(SessionOptionalContext),
    space = useOptionalSpace();
  return session?.user && (!space || (space.ready && space.current?.kind === 'personal')) ? (
    <DailyGiftClaimContent />
  ) : null;
}
function DailyGiftClaimContent() {
  const { c } = useCatalogText();
  const { service, user } = useSession(),
    credits = usePersonalCredits();
  const [date, setDate] = useState(shanghaiDate),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false);
  const scope = [user?.identity, date, sessionBoundary.epoch].join(':'),
    active = useRef(false),
    current = useRef(scope),
    flight = useRef(''),
    confirmed = useRef('');
  current.current = scope;
  const refreshRef = useRef(credits?.refresh);
  refreshRef.current = credits?.refresh;
  const claim = useCallback(async () => {
    if (!user || flight.current === scope || confirmed.current === scope) return;
    const epoch = sessionBoundary.epoch;
    flight.current = scope;
    setBusy(true);
    setError('');
    try {
      const receipt = await service.dailyGift();
      sessionBoundary.assert(epoch);
      if (!active.current || current.current !== scope) return;
      // 0表示尚未领取，不能阻断当日开启额度后的再次核对。
      if (receipt.claimed) confirmed.current = scope;
      if (!receipt.alreadyClaimed && BigInt(receipt.amount) > 0n) {
        refreshRef.current?.();
        toast.success(
          translate('今日赠送 {amount} 积分已到账', {
            amount: formatInteger(receipt.amount),
          }),
          {
            id: 'daily-credit-gift',
          },
        );
      }
    } catch (e) {
      if (active.current && current.current === scope && epoch === sessionBoundary.epoch)
        setError((e as Error).message || translate('每日赠送暂时无法确认，请重试核对。'));
    } finally {
      if (flight.current === scope) flight.current = '';
      if (active.current && current.current === scope && epoch === sessionBoundary.epoch)
        setBusy(false);
    }
  }, [service, user?.identity, scope]);
  useEffect(() => {
    active.current = true;
    void claim();
    return () => {
      active.current = false;
    };
  }, [claim]);
  useEffect(() => {
    const check = () => {
      if (document.visibilityState !== 'visible') return;
      const today = shanghaiDate();
      if (today !== date) setDate(today);
      else if (!error) void claim();
    };
    const nextMidnight = new Date(shanghaiDate() + 'T00:00:00+08:00').getTime() + 86400000;
    const timer = setTimeout(check, Math.max(100, nextMidnight - Date.now() + 50));
    window.addEventListener('focus', check);
    document.addEventListener('visibilitychange', check);
    return () => {
      clearTimeout(timer);
      window.removeEventListener('focus', check);
      document.removeEventListener('visibilitychange', check);
    };
  }, [date, error, claim]);
  if (!error) return null;
  return (
    <div className="daily-credit-feedback" role="alert">
      <span>{c('每日赠送未确认：{error}', { error })}</span>
      <Button variant="ghost" size="sm" busy={busy} onClick={() => void claim()}>
        {c('重试核对')}
      </Button>
    </div>
  );
}
