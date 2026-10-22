import { useContext, useState } from 'react';
import { Link } from 'react-router-dom';
import { paymentApi } from '../../api/payment-client';
import type { PaymentAPI } from '../../api/payment-types';
import { sessionBoundary } from '../../api/session-boundary';
import { SessionOptionalContext } from '../../state/SessionContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { paymentStatusLabel, paymentTargetLabel } from '../../domain/payment-format';
import { dateTime, formatInteger } from '../credits/credit-format';
import { Button } from '../../components/ui/button';
import { Choice, Feedback, useRead } from '../operations/controls';
import { PaymentPrice } from '../../features/payments/PaymentPrice';
import { RechargeButton } from '../../features/payments/Recharge';

export function RechargeOrdersPage({ api = paymentApi }: { api?: Pick<PaymentAPI, 'orders'> }) {
  const { c } = useCatalogText();
  const session = useContext(SessionOptionalContext);
  const [page, setPage] = useState(1),
    [pageSize, setPageSize] = useState(10);
  const read = useRead(
    (signal) => api.orders(page, pageSize, signal),
    [api, page, pageSize, session?.service, session?.user?.identity, sessionBoundary.epoch],
    !!session?.user,
  );
  const result = read.value;
  return (
    <main className="payment-page">
      <header className="payment-page-head">
        <h1>{c('充值记录')}</h1>
        <div>
          <Link to="/credits">{c('积分明细')}</Link>
          <RechargeButton />
        </div>
      </header>
      <p className="payment-page-lead">
        {c('仅显示本人下单的充值记录。需要发票请联系客服并提供订单号。')}
      </p>
      <Feedback
        busy={read.busy}
        error={read.error}
        empty={result?.items.length === 0}
        retry={read.refresh}
      />
      {result ? (
        <>
          {result.items.length ? (
            <div className="payment-records" role="table" aria-label={c('充值记录')}>
              <div className="payment-records-columns" role="row">
                <span role="columnheader">{c('时间')}</span>
                <span role="columnheader">{c('充值到')}</span>
                <span role="columnheader">{c('获得积分')}</span>
                <span role="columnheader">{c('实付金额')}</span>
                <span role="columnheader">{c('状态')}</span>
              </div>
              {/* 只保留用户关心的关键信息；渠道交易号、协议版本等审计事实留给运营端。 */}
              {result.items.map((order) => (
                <div className="payment-records-row" role="row" key={order.orderNo}>
                  <time role="cell" dateTime={order.createdAt}>
                    {dateTime(order.createdAt)}
                  </time>
                  <span role="cell" className="payment-records-target">
                    <strong>{paymentTargetLabel(order.target, c)}</strong>
                    <small title={order.orderNo}>{c('订单号 {no}', { no: order.orderNo })}</small>
                  </span>
                  <span role="cell" className="payment-records-credits">
                    +{formatInteger(String(order.rechargeCredits + order.giftCredits))}
                    {order.giftCredits > 0 ? (
                      <small>
                        {c('含赠送 {credits}', { credits: formatInteger(String(order.giftCredits)) })}
                      </small>
                    ) : null}
                  </span>
                  <span role="cell" className="payment-records-amount">
                    <PaymentPrice amountFen={order.amountFen} originalAmountFen={order.originalAmountFen} />
                  </span>
                  <span role="cell" className="payment-records-status" data-status={order.status}>
                    {paymentStatusLabel(order.status, c)}
                  </span>
                </div>
              ))}
            </div>
          ) : null}
          <nav className="payment-pagination" aria-label={c('充值记录分页')}>
            <div className="payment-page-size-field">
              {c('每页条数')}{' '}
              <Choice
                label={c('充值记录每页条数')}
                value={String(pageSize)}
                disabled={read.busy}
                className="payment-choice payment-page-size"
                menuClassName="payment-choice-menu"
                onChange={(value) => {
                  setPageSize(Number(value));
                  setPage(1);
                }}
                options={[10, 20, 50].map((value) => ({
                  value: String(value),
                  label: String(value),
                }))}
              />
            </div>
            <span>
              {c('第 {page} 页，共 {total} 条', { page: result.page, total: result.total })}
            </span>
            <Button
              size="sm"
              variant="outline"
              disabled={read.busy || page <= 1}
              onClick={() => setPage(page - 1)}
            >
              {c('上一页')}
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={read.busy || page * pageSize >= result.total}
              onClick={() => setPage(page + 1)}
            >
              {c('下一页')}
            </Button>
          </nav>
        </>
      ) : null}
    </main>
  );
}
