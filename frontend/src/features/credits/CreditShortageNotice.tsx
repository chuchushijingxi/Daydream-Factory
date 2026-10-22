import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useOptionalSpace } from '../../state/SpaceContext';
import { teamCreditsApi } from '../../api/team-credits';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Button } from '../../components/ui/button';
import { RechargeButton, useRecharge } from '../payments/Recharge';
export function CreditShortageNotice({ code, needed }: { code?: string | null; needed?: number }) {
  const { c } = useCatalogText(),
    space = useOptionalSpace(),
    recharge = useRecharge();
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [sent, setSent] = useState(false);
  useEffect(() => {
    setSent(false);
    setError('');
  }, [space?.spaceId, code, needed]);
  if (
    space?.current?.kind !== 'team' ||
    !['MEMBER_CREDITS_INSUFFICIENT', 'TEAM_UNALLOCATED_INSUFFICIENT'].includes(code || '')
  )
    return null;
  const request = async () => {
    if (!needed || busy) return;
    setBusy(true);
    setError('');
    try {
      await teamCreditsApi.request(space.spaceId, String(needed));
      setSent(true);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="jm-inline-error" role="status">
      {code === 'MEMBER_CREDITS_INSUFFICIENT' ? (
        <>
          <span>
            {space.current.role === 'admin'
              ? c('名下积分不足，请联系所有者分配。')
              : c('名下积分不足，请联系管理员分配。')}
          </span>
          <Button variant="link" disabled={busy || sent || !needed} onClick={() => void request()}>
            {c('申请积分')}
          </Button>
          <Link to="/credits">{c('查看积分明细')}</Link>
        </>
      ) : space.current.role === 'owner' && recharge?.available ? (
        <>
          <span>{c('未分配积分不足，请先充值。')}</span>
          <RechargeButton />
        </>
      ) : space.current.role === 'owner' ? (
        <span>{c('未分配积分不足；充值暂不可用，请联系运营。')}</span>
      ) : (
        <span>{c('未分配积分不足时，请联系所有者充值。')}</span>
      )}
      {sent ? <span>{c('积分申请通知已发送')}</span> : null}
      {error ? <span role="alert">{error}</span> : null}
    </div>
  );
}
