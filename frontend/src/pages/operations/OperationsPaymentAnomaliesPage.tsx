import { useState, type FormEvent } from 'react';
import {
  paymentApi,
  type PaymentAPI,
  type PaymentAnomaly,
  type PaymentAnomalyFilters,
} from '../../api/payment-client';
import { paymentChannelLabel } from '../../domain/payment-format';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '../../components/ui/dialog';
import { Choice, Feedback, Field, useRead } from './controls';
import { dateTime } from '../credits/credit-format';
import { PaymentOrderDialog } from './OperationsPaymentOrdersPage';
import {
  PaymentFacts,
  PaymentMessages,
  PaymentPagination,
  paymentAnomalyLabel,
  usePaymentAction,
  usePaymentEpoch,
  usePaymentRefundDrafts,
} from './payment-controls';
const anomalyKinds = [
  'signature_invalid',
  'notification_invalid',
  'identity_mismatch',
  'amount_mismatch',
  'paid_after_close',
  'bill_missing_local',
  'bill_missing_channel',
  'bill_amount_mismatch',
  'refund_mismatch',
  'refund_failed',
];
export function OperationsPaymentAnomaliesPage({
  api = paymentApi,
  embedded = false,
}: {
  api?: PaymentAPI;
  embedded?: boolean;
}) {
  const epoch = usePaymentEpoch();
  return <AnomaliesScope key={epoch} api={api} embedded={embedded} />;
}
function AnomaliesScope({ api, embedded }: { api: PaymentAPI; embedded: boolean }) {
  const { c } = useCatalogText(),
    [filters, setFilters] = useState<PaymentAnomalyFilters>({}),
    [page, setPage] = useState(1),
    [size, setSize] = useState(20),
    [selected, setSelected] = useState<PaymentAnomaly | null>(null),
    [orderNo, setOrderNo] = useState('');
  const refunds = usePaymentRefundDrafts();
  const read = useRead(
    (signal) => api.anomalies(filters, page, size, signal),
    [api, filters, page, size],
  );
  function filter(key: keyof PaymentAnomalyFilters, value: string) {
    setFilters({ ...filters, [key]: value || undefined });
    setPage(1);
  }
  return (
    <section className={embedded ? 'op-payment-page' : 'op-page op-payment-page'}>
      {!embedded ? (
        <header className="op-page-heading">
          <div>
            <h1>{c('支付异常')}</h1>
            <p>{c('异常仅支持重新查单与标记已核查，不提供手工加积分。')}</p>
          </div>
        </header>
      ) : null}
      <div className="op-filters">
        <Field label={c('异常状态')}>
          <Choice
            label={c('异常状态')}
            value={filters.status || ''}
            onChange={(value) => filter('status', value)}
            className="payment-choice"
            menuClassName="payment-choice-menu"
            options={[
              { value: '', label: c('全部状态') },
              { value: 'open', label: c('待核查') },
              { value: 'resolved', label: c('已核查') },
            ]}
          />
        </Field>
        <Field label={c('异常类型')}>
          <Choice
            label={c('异常类型')}
            value={filters.kind || ''}
            onChange={(value) => filter('kind', value)}
            className="payment-choice"
            menuClassName="payment-choice-menu"
            options={[
              { value: '', label: c('全部异常类型') },
              ...anomalyKinds.map((value) => ({ value, label: paymentAnomalyLabel(value, c) })),
            ]}
          />
        </Field>
        <Field label={c('支付渠道')}>
          <Choice
            label={c('支付渠道')}
            value={filters.channel || ''}
            onChange={(value) => filter('channel', value)}
            className="payment-choice"
            menuClassName="payment-choice-menu"
            options={[
              { value: '', label: c('全部渠道') },
              ...(['demo', 'wechatpay', 'alipay'] as const).map((value) => ({
                value,
                label: paymentChannelLabel(value, c),
              })),
            ]}
          />
        </Field>
        <Button variant="outline" onClick={read.refresh}>
          {c('刷新')}
        </Button>
      </div>
      <PaymentMessages error={refunds.error} notice="" />
      <Feedback busy={read.busy} error={read.error} retry={read.refresh} />
      {read.value ? (
        <>
          <div className="op-table-scroll" tabIndex={0} aria-label={c('支付异常列表')}>
            <table className="op-table">
              <thead>
                <tr>
                  {[c('发生时间'), c('异常类型'), c('订单号'), c('渠道'), c('状态'), c('操作')].map(
                    (v) => (
                      <th key={v}>{v}</th>
                    ),
                  )}
                </tr>
              </thead>
              <tbody>
                {!read.value.items.length ? (
                  <tr>
                    <td colSpan={6} className="op-payment-empty" role="status">
                      {c('当前条件下暂无记录')}
                    </td>
                  </tr>
                ) : null}
                {read.value.items.map((v) => (
                  <tr key={v.id}>
                    <td>{dateTime(v.createdAt)}</td>
                    <td>{paymentAnomalyLabel(v.kind, c)}</td>
                    <td>{v.orderNo || '—'}</td>
                    <td>{paymentChannelLabel(v.channel, c)}</td>
                    <td>{v.status === 'open' ? c('待核查') : c('已核查')}</td>
                    <td>
                      <Button size="sm" variant="outline" onClick={() => setSelected(v)}>
                        {c('核查详情')}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <PaymentPagination
            page={read.value}
            busy={read.busy}
            onPage={setPage}
            pageSize={size}
            onSize={(v) => {
              setSize(v);
              setPage(1);
            }}
          />
        </>
      ) : null}
      <Dialog
        open={!!selected}
        onOpenChange={(v) => {
          if (!v) setSelected(null);
        }}
      >
        <DialogContent variant="panel" className="op-payment-detail">
          <DialogTitle>{c('支付异常详情')}</DialogTitle>
          <DialogDescription>{c('核查说明只追加审计，不会直接修改钱包余额。')}</DialogDescription>
          {selected ? (
            <AnomalyDetails
              key={selected.id}
              value={selected}
              api={api}
              onChange={(v) => {
                setSelected(v);
                read.refresh();
              }}
              onOrder={setOrderNo}
            />
          ) : null}
        </DialogContent>
      </Dialog>
      <PaymentOrderDialog
        api={api}
        orderNo={orderNo}
        refunds={refunds}
        onClose={() => setOrderNo('')}
      />
    </section>
  );
}
function AnomalyDetails({
  value,
  api,
  onChange,
  onOrder,
}: {
  value: PaymentAnomaly;
  api: PaymentAPI;
  onChange: (v: PaymentAnomaly) => void;
  onOrder: (v: string) => void;
}) {
  const { c } = useCatalogText(),
    action = usePaymentAction(),
    [note, setNote] = useState(''),
    [reason, setReason] = useState('');
  async function resolve(e: FormEvent) {
    e.preventDefault();
    if (!note.trim()) return;
    await action.run(async (current) => {
      const result = await api.resolveAnomaly(value.id, note.trim());
      if (current()) {
        onChange(result);
        action.setNotice(c('已记录核查说明。'));
      }
    });
  }
  async function query(e: FormEvent) {
    e.preventDefault();
    if (!reason.trim() || !value.orderNo) return;
    await action.run(async (current) => {
      await api.queryOrder(value.orderNo!, reason.trim());
      if (current()) action.setNotice(c('查单已完成，请在订单详情核对结果。'));
    });
  }
  return (
    <>
      <dl className="op-detail-grid">
        {[
          [c('异常编号'), value.id],
          [c('异常类型'), paymentAnomalyLabel(value.kind, c)],
          [c('状态'), value.status === 'open' ? c('待核查') : c('已核查')],
          [c('发生时间'), dateTime(value.createdAt)],
          [c('订单号'), value.orderNo || '—'],
          [c('渠道'), paymentChannelLabel(value.channel, c)],
          [c('核查人'), value.resolvedBy || '—'],
          [c('核查时间'), dateTime(value.resolvedAt)],
        ].map(([label, v]) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd>{v}</dd>
          </div>
        ))}
      </dl>
      <PaymentFacts value={value.detail} label={c('异常事实')} />
      {value.note ? <p className="op-payment-reason">{value.note}</p> : null}
      {value.orderNo ? (
        <>
          <Button variant="outline" onClick={() => onOrder(value.orderNo!)}>
            {c('查看关联订单')}
          </Button>
          <form className="op-payment-action" onSubmit={query}>
            <Field label={c('重新查单原因')}>
              <textarea
                className="jm-field-textarea"
                required
                value={reason}
                maxLength={2000}
                disabled={action.busy}
                onChange={(e) => setReason(e.target.value)}
              />
            </Field>
            <Button type="submit" busy={action.busy} disabled={!reason.trim()}>
              {c('重新查单')}
            </Button>
          </form>
        </>
      ) : (
        <p className="op-muted">{c('该异常未关联订单，不能重新查单。')}</p>
      )}
      {value.status === 'open' ? (
        <form className="op-payment-action" onSubmit={resolve}>
          <Field label={c('核查说明')}>
            <textarea
              className="jm-field-textarea"
              required
              value={note}
              maxLength={2000}
              disabled={action.busy}
              onChange={(e) => setNote(e.target.value)}
            />
          </Field>
          <Button type="submit" busy={action.busy} disabled={!note.trim()}>
            {c('标记已核查')}
          </Button>
        </form>
      ) : null}
      <PaymentMessages error={action.error} notice={action.notice} />
    </>
  );
}
