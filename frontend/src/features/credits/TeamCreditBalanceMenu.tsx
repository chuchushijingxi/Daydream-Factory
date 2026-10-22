import { useRef, useState } from 'react';
import * as Popover from '@radix-ui/react-popover';
import { Link } from 'react-router-dom';
import { holdsCredits, teamCreditsApi } from '../../api/team-credits';
import { useCreditRead } from './useCreditRead';
import { useTeamCredits } from './PersonalCredits';
import { useOptionalSpace } from '../../state/SpaceContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { CreditStar } from '../../components/ui/credit-star';
import { Spinner } from '../../components/ui/spinner';
import { formatInteger } from '../../pages/credits/credit-format';
import {
  allocationLabels,
  shortCreditTime,
  teamCreditTitle,
} from '../../pages/credits/personal-credit-format';
import { teamCreditsPath } from '../../pages/credits/TeamCreditsPage';
import { RechargeButton } from '../payments/Recharge';

export function TeamCreditBalanceMenu() {
  const { c } = useCatalogText();
  const credits = useTeamCredits();
  const space = useOptionalSpace();
  const [open, setOpen] = useState(false);
  const [requested, setRequested] = useState(false);
  // 成员与管理员的顶栏余额是本人名下分配额，最近记录与明细也看本人；所有者看团队。
  const holder = holdsCredits(space?.current?.role);
  // 与充值目标同一规则：只有所有者可给未到期、未停用的团队充值；管理员和成员不显示入口。
  const rechargeable = space?.current?.role === 'owner' && space.current.status === 'active';
  const openingRecharge = useRef(false);
  const teamName = space?.current?.name || '';
  const amount = credits?.balance == null ? '—' : formatInteger(credits.balance);
  const recent = useCreditRead(
    () => teamCreditsApi.recent(holder),
    [credits?.revision, space?.spaceId, holder],
    requested,
  );
  const change = (next: boolean) => {
    setOpen(next);
    if (next && !open) {
      setRequested(true);
      credits?.refresh();
    }
  };
  // 分配记录仍是本人名下的变动，不冒充团队账本；合并只用于按时间展示最近记录。
  const records = [
    ...(recent.value?.items.map((item) => ({
      key: 'ledger:' + item.id,
      title: teamCreditTitle(item, c),
      amount: item.amount,
      direction: item.kind,
      createdAt: item.createdAt,
    })) || []),
    ...(holder
      ? recent.value?.allocations.map((item) => ({
          key: 'allocation:' + item.id,
          title: c(allocationLabels[item.kind]),
          amount: item.amount,
          direction: item.kind === 'allocate' ? 'add' : 'deduct',
          createdAt: item.createdAt,
        })) || []
      : []),
  ]
    .sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt))
    .slice(0, 5);

  return (
    <Popover.Root modal={false} open={open} onOpenChange={change}>
      <Popover.Trigger asChild>
        <button
          type="button"
          className="credit-balance-trigger team-credit-trigger"
          aria-label={c('{amount} 积分｜{name}', {
            amount: credits?.error ? c('读取失败') : amount,
            name: teamName,
          })}
          onKeyDown={(event) => {
            if (event.key === 'ArrowDown') {
              event.preventDefault();
              change(true);
            }
          }}
        >
          <CreditStar />
          <span className="credit-trigger-amount">{amount}</span>
          <span>{c('积分')}</span>
          <span className="credit-trigger-divider" aria-hidden />
          <span className="team-credit-trigger-name">{teamName}</span>
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          className="personal-credit-panel team-credit-panel"
          align="end"
          sideOffset={6}
          collisionPadding={12}
          aria-label={c('团队积分')}
          onCloseAutoFocus={(event) => {
            if (openingRecharge.current) {
              event.preventDefault();
              openingRecharge.current = false;
            }
          }}
        >
          {/* 排版与个人空间积分浮层一致：余额在左，充值入口在右。 */}
          <div className="credit-panel-balance credit-panel-balance-recharge">
            <div className="credit-panel-balance-info">
              <span>{c('积分余额')}</span>
              <strong data-long={amount.length > 14}>{amount}</strong>
            </div>
            {rechargeable ? (
              <RechargeButton
                className="credit-panel-recharge"
                onClick={() => {
                  openingRecharge.current = true;
                  change(false);
                }}
              />
            ) : null}
          </div>
          {credits?.syncError ? (
            <p className="credit-panel-feedback" role="status">
              {credits.syncError}
            </p>
          ) : null}
          {credits?.error ? (
            <div className="credit-panel-error" role="alert">
              <p>{credits.error}</p>
              <button type="button" className="credit-retry" onClick={credits.refresh}>
                {c('重新读取余额')}
              </button>
            </div>
          ) : null}
          <div className="credit-panel-records-heading">
            <span>{c('积分记录')}</span>
            {/* 所有者的明细就是团队页积分分区；成员与管理员看本人在团队中的积分页。 */}
            <Link
              to={space && !holder ? teamCreditsPath(space.spaceId) : '/credits'}
              onClick={() => change(false)}
            >
              {c('查看积分明细')}
            </Link>
          </div>
          <div className="credit-panel-records" aria-busy={recent.busy}>
            {recent.busy && recent.value === null ? (
              <div className="credit-panel-feedback" role="status">
                <Spinner />
                {c('读取积分记录…')}
              </div>
            ) : null}
            {recent.error ? (
              <div className="credit-panel-error" role="alert">
                <p>{recent.error}</p>
                <button type="button" className="credit-retry" onClick={recent.refresh}>
                  {c('重新读取记录')}
                </button>
              </div>
            ) : null}
            {records.length ? (
              <ul>
                {records.map((entry) => (
                  <li key={entry.key}>
                    <span>
                      <strong title={entry.title}>{entry.title}</strong>
                      <time dateTime={entry.createdAt}>{shortCreditTime(entry.createdAt)}</time>
                    </span>
                    <span className="credit-record-amount" data-direction={entry.direction}>
                      {['deduct', 'expire'].includes(entry.direction) ? '−' : '+'}
                      {formatInteger(entry.amount)}
                    </span>
                  </li>
                ))}
              </ul>
            ) : recent.value !== null && !recent.error ? (
              <div className="credit-panel-empty" role="status">
                {c('暂无积分记录')}
              </div>
            ) : null}
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
