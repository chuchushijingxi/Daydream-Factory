import type { ReactNode } from 'react';
import type { SpaceRole } from '../../api/spaces-types';
import { useTeamCredits } from '../../features/credits/PersonalCredits';
import { RechargeButton } from '../../features/payments/Recharge';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Feedback } from '../operations/controls';
import { formatInteger } from '../credits/credit-format';
import { CreditStar } from '../../components/ui/credit-star';
import type { TeamView } from '../../api/spaces-types';
import { TeamCreditRecords } from './TeamCreditRecords';
import { dateOnly, numberText } from './team-ui';

// 管理者只花未分配（右上角积分入口显示的也是它），所以未分配是主数字；
// 总余额只作为“未分配 + 成员名下未使用”的合计出现在构成条里，避免两个大数并列。
export function BalanceTiles({
  id,
  role,
  children,
}: {
  id: string;
  role: SpaceRole;
  children?: ReactNode;
}) {
  const { c } = useCatalogText();
  const credits = useTeamCredits();
  const wallet = credits?.wallet?.spaceId === id ? credits.wallet : null;
  const free = wallet?.unallocated ?? '0',
    held = wallet?.allocated ?? '0',
    total = wallet?.totalBalance ?? '0';
  // 金额是十进制整数文本；构成条只需相对宽度，BigInt 先缩到安全范围再转 Number。
  const share = (part: string) => {
    const whole = BigInt(total);
    return whole > 0n ? Number((BigInt(part) * 10000n) / whole) / 100 : 0;
  };
  return (
    <>
      <Feedback
        busy={Boolean(credits?.busy && !wallet)}
        error={credits?.error || ''}
        retry={() => credits?.refresh()}
      />
      {credits?.syncError && (
        <p role="status" className="op-muted">
          {credits.syncError}
        </p>
      )}
      {wallet && wallet.role !== 'member' && (
        <div className="team-tiles team-balance-row">
          <section className="team-tile team-balance-card" aria-label={c('团队积分')}>
            <div className="team-balance-head">
              <span className="team-tile-label">{c('未分配积分')}</span>
              {role === 'owner' ? (
                <RechargeButton />
              ) : (
                <span className="team-tile-hint">{c('不足时请联系所有者充值')}</span>
              )}
            </div>
            <strong className="team-tile-value team-balance-value">
              <CreditStar />
              {formatInteger(free)}
            </strong>
            <div
              className="team-balance-bar"
              role="img"
              aria-label={c(
                '团队总积分 {total}：未分配 {free}（即右上角显示的积分），已分配给成员 {held}',
                {
                  total: formatInteger(total),
                  free: formatInteger(free),
                  held: formatInteger(held),
                },
              )}
            >
              <span data-part="free" style={{ width: share(free) + '%' }} />
              <span data-part="held" style={{ width: share(held) + '%' }} />
            </div>
            {/* 先给总量，再按构成条从左到右列出两部分：总积分 = 未分配 + 已分配给成员。 */}
            <dl className="team-balance-legend">
              <div data-part="total">
                <dt>{c('团队总积分')}</dt>
                <dd>{formatInteger(total)}</dd>
              </div>
              <div data-part="free">
                <dt>{c('未分配')}</dt>
                <dd>{formatInteger(free)}</dd>
              </div>
              <div data-part="held">
                <dt>{c('已分配给成员')}</dt>
                <dd>{formatInteger(held)}</dd>
              </div>
            </dl>
          </section>
          {children}
        </div>
      )}
    </>
  );
}

export function CreditsPanel({
  id,
  role,
  team,
}: {
  id: string;
  role: SpaceRole;
  team?: TeamView;
}) {
  const { c } = useCatalogText();
  return (
    <div className="team-stack team-credit-panel">
      <BalanceTiles id={id} role={role}>
        {team && (
          <div className="team-tile">
            <span className="team-tile-label">{c('下次发放')}</span>
            <strong className="team-tile-value">+{numberText(team.grantAmount)}</strong>
            <span className="team-tile-hint">
              {c('{date} 自动发放到未分配，每 30 天一次', { date: dateOnly(team.nextGrantAt) })}
            </span>
          </div>
        )}
      </BalanceTiles>
      <TeamCreditRecords />
    </div>
  );
}
