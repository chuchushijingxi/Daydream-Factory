import { useContext, useState } from 'react';
import { ReceiptText } from 'lucide-react';
import { paymentApi } from '../../api/payment-client';
import type { PaymentAPI } from '../../api/payment-types';
import { sessionBoundary } from '../../api/session-boundary';
import { SessionOptionalContext } from '../../state/SessionContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { paymentStatusLabel, paymentTargetLabel } from '../../domain/payment-format';
import { dateTime, formatInteger } from '../credits/credit-format';
import { Choice, Feedback, PageNavigation, useRead } from '../operations/controls';
import { PaymentPrice } from '../../features/payments/PaymentPrice';
import { RechargeButton } from '../../features/payments/Recharge';

// 版式沿用积分明细页：同宽内容区、标题说明、白卡列表与卡内紧凑分页。
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
    <section className="personal-credits-page payment-orders-page">
      <div className="personal-credits-content">
        <header className="personal-credits-heading">
          <div>
            <div className="personal-credits-title">
              <h1>{c('充值记录')}</h1>
            </div>
            <p>{c('仅显示本人下单的充值记录。需要发票请联系客服并提供订单号。')}</p>
          </div>
          <RechargeButton />
        </header>
        <div className="personal-credit-record-card" aria-busy={read.busy}>
          <Feedback busy={!result && read.busy} error={read.error} retry={read.refresh} />
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
                        <strong>
                          +{formatInteger(String(order.rechargeCredits + order.giftCredits))}
                        </strong>
                        {order.giftCredits > 0 ? (
                          <small>
                            {c('含赠送 {credits}', {
                              credits: formatInteger(String(order.giftCredits)),
                            })}
                          </small>
                        ) : null}
                      </span>
                      <span role="cell" className="payment-records-amount">
                        <PaymentPrice
                          amountFen={order.amountFen}
                          originalAmountFen={order.originalAmountFen}
                        />
                      </span>
                      <span role="cell" className="payment-records-state">
                        <span className="payment-records-status" data-status={order.status}>
                          {paymentStatusLabel(order.status, c)}
                        </span>
                      </span>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="personal-credit-empty">
                  <ReceiptText aria-hidden />
                  <h3>{c('暂无充值记录')}</h3>
                  <p>{c('完成充值后，可在这里查看订单与到账状态。')}</p>
                </div>
              )}
              <div className="credit-pagination">
                <Choice
                  label={c('充值记录每页条数')}
                  className="credit-page-size"
                  value={String(pageSize)}
                  disabled={read.busy}
                  options={['10', '20', '50'].map((size) => ({
                    value: size,
                    label: c('{size} 条 / 页', { size }),
                  }))}
                  onChange={(value) => {
                    setPageSize(Number(value));
                    setPage(1);
                  }}
                />
                <PageNavigation
                  page={{
                    items: result.items,
                    previous: result.page > 1 ? String(result.page - 1) : null,
                    next:
                      result.page * result.pageSize < result.total ? String(result.page + 1) : null,
                    caption: c('共 {total} 条 · 第 {page} 页', {
                      total: result.total.toLocaleString('zh-CN'),
                      page: result.page,
                    }),
                  }}
                  busy={read.busy}
                  onPage={(next) => setPage(Number(next))}
                />
              </div>
            </>
          ) : null}
        </div>
      </div>
    </section>
  );
}
