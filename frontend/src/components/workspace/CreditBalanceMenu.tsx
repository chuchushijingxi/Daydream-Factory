import { useOptionalSpace } from '../../state/SpaceContext';
import { TeamCreditBalanceMenu } from '../../features/credits/TeamCreditBalanceMenu';
import { useContext, useRef, useState } from 'react';
import * as Popover from '@radix-ui/react-popover';
import { Link } from 'react-router-dom';
import { CreditStar } from '../ui/credit-star';
import { SessionOptionalContext } from '../../state/SessionContext';
import { usePersonalCredits } from '../../features/credits/PersonalCredits';
import { useCreditRead } from '../../features/credits/useCreditRead';
import { sessionBoundary } from '../../api/session-boundary';
import type { Administration } from '../../api/administration';
import { formatInteger } from '../../pages/credits/credit-format';
import {
  creditDirectionLabels,
  creditRecordTitle,
  creditSignedAmount,
  shortCreditTime,
} from '../../pages/credits/personal-credit-format';
import { Spinner } from '../ui/spinner';
import { useCatalogText } from '../../contexts/useCatalogText';
import { RechargeButton } from '../../features/payments/Recharge';

export function CreditBalanceMenu() {
  const space = useOptionalSpace();
  return space?.current?.kind === 'team' ? (
    <TeamCreditBalanceMenu />
  ) : (
    <PersonalCreditBalanceMenu />
  );
}
function PersonalCreditBalanceMenu() {
  const { c } = useCatalogText();
  const session = useContext(SessionOptionalContext),
    balance = usePersonalCredits();
  const [open, setOpen] = useState(false);
  const openingRecharge = useRef(false);
  const [requestedScope, setRequestedScope] = useState<{
    service: Administration;
    identity: string;
    epoch: number;
  } | null>(null);
  // 关闭不丢弃已确认的五条记录；仅已打开过的当前会话随积分通知更新。
  const recent = useCreditRead(
    () => session!.service.myRecentLedger(),
    [balance?.revision],
    !!session?.user &&
      requestedScope?.service === session.service &&
      requestedScope.identity === session.user.identity &&
      requestedScope.epoch === sessionBoundary.epoch,
  );
  if (!balance?.enabled) return null;
  const amount = balance.balance === null ? '—' : formatInteger(balance.balance);
  const change = (next: boolean) => {
    setOpen(next);
    if (next && !open && session?.user) {
      setRequestedScope({
        service: session.service,
        identity: session.user.identity,
        epoch: sessionBoundary.epoch,
      });
      balance.refresh();
    }
  };
  return (
    <Popover.Root modal={false} open={open} onOpenChange={change}>
      <Popover.Trigger asChild>
        <button
          type="button"
          className="credit-balance-trigger"
          aria-label={c('积分余额 {amount}', {
            amount: balance.error ? c('读取失败') : amount,
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
          <span className="credit-trigger-divider" aria-hidden />
          <span>{c('积分')}</span>
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          className="personal-credit-panel"
          align="end"
          sideOffset={6}
          collisionPadding={12}
          aria-label={c('积分记录')}
          onCloseAutoFocus={(event) => {
            if (openingRecharge.current) {
              event.preventDefault();
              openingRecharge.current = false;
            }
          }}
        >
          <div className="credit-panel-balance credit-panel-balance-recharge">
            <div className="credit-panel-balance-info">
              <span>{c('积分余额')}</span>
              <strong data-long={amount.length > 14}>{amount}</strong>
            </div>
            <RechargeButton
              className="credit-panel-recharge"
              onClick={() => {
                openingRecharge.current = true;
                change(false);
              }}
            />
          </div>
          {balance.syncError ? (
            <p className="credit-panel-feedback" role="status">
              {balance.syncError}
            </p>
          ) : null}
          {balance.error ? (
            <div className="credit-panel-error" role="alert">
              <p>{balance.error}</p>
              <button type="button" className="credit-retry" onClick={balance.refresh}>
                {c('重新读取余额')}
              </button>
            </div>
          ) : null}
          <div className="credit-panel-records-heading">
            <span>{c('积分记录')}</span>
            <Link to="/credits" onClick={() => change(false)}>
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
            {recent.value?.length ? (
              <ul>
                {recent.value.map((entry) => (
                  <li key={entry.identity}>
                    <span>
                      <strong title={creditRecordTitle(entry, c)}>
                        {creditRecordTitle(entry, c)}
                      </strong>
                      <time dateTime={entry.at}>{shortCreditTime(entry.at)}</time>
                    </span>
                    <span
                      className="credit-record-amount"
                      data-direction={entry.direction}
                      aria-label={c('{action} {amount} 积分', {
                        action: c(creditDirectionLabels[entry.direction]),
                        amount: formatInteger(entry.amount),
                      })}
                    >
                      {creditSignedAmount(entry)}
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
