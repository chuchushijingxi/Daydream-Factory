import { useState, type FormEvent } from 'react';
import {
  paymentApi,
  type PaymentAPI,
  type PaymentOrderFilters,
  type PaymentRefundQuote,
} from '../../api/payment-client';
import {
  paymentChannelLabel,
  paymentStatusLabel,
  paymentTargetLabel,
  formatPaymentAmount,
  paymentCreditsInput,
} from '../../domain/payment-format';
import { writeResultUnknown } from '../../api/errors';
import { useCatalogText } from '../../contexts/useCatalogText';
import { DateTimePicker } from '../../components/ui/date-time-picker';
import { PaymentPrice } from '../../features/payments/PaymentPrice';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '../../components/ui/dialog';
import { Choice, Feedback, Field, useRead } from './controls';
import { dateTime, formatInteger, shanghaiInput } from '../credits/credit-format';
import { Ledger } from '../credits/CreditsPage';
import {
  PaymentFacts,
  PaymentMessages,
  PaymentPagination,
  paymentEventLabel,
  usePaymentAction,
  usePaymentEpoch,
  usePaymentRefundDrafts,
  type PaymentRefundCommand,
  type PaymentRefundDrafts,
} from './payment-controls';
type RefundCommand = PaymentRefundCommand;
type RefundDrafts = PaymentRefundDrafts;
const emptyFilters = {
  orderNo: '',
  channelTradeNo: '',
  buyerUserId: '',
  targetKind: '',
  targetId: '',
  channel: '',
  status: '',
  start: '',
  end: '',
};
export function OperationsPaymentOrdersPage({
  api = paymentApi,
  embedded = false,
}: {
  api?: PaymentAPI;
  embedded?: boolean;
}) {
  const epoch = usePaymentEpoch();
  return <OrdersScope key={epoch} api={api} embedded={embedded} />;
}
function OrdersScope({ api, embedded }: { api: PaymentAPI; embedded: boolean }) {
  const { c } = useCatalogText(),
    [draft, setDraft] = useState(emptyFilters),
    [filters, setFilters] = useState<PaymentOrderFilters>({}),
    [page, setPage] = useState(1),
    [pageSize, setPageSize] = useState(20),
    [selected, setSelected] = useState(''),
    [error, setError] = useState('');
  const refunds = usePaymentRefundDrafts();
  const read = useRead(
    (signal) => api.operationsOrders(filters, page, pageSize, signal),
    [api, filters, page, pageSize],
  );
  function search(event: FormEvent) {
    event.preventDefault();
    try {
      const values = Object.fromEntries(
        Object.entries(draft)
          .map(([k, v]) => [k, v.trim()])
          .filter(([, v]) => v),
      );
      if (draft.start) values.start = shanghaiInput(draft.start);
      if (draft.end) values.end = shanghaiInput(draft.end);
      if (values.start && values.end && values.start >= values.end)
        throw new Error(c('结束时间须晚于开始时间。'));
      setFilters(values as PaymentOrderFilters);
      setPage(1);
      setError('');
    } catch (e) {
      setError((e as Error).message);
    }
  }
  const textFilters = [
    ['orderNo', c('订单号')],
    ['channelTradeNo', c('渠道交易号')],
    ['buyerUserId', c('下单人 ID')],
    ['targetId', c('目标空间 ID')],
  ] as const;
  return (
    <section className={embedded ? 'op-payment-page' : 'op-page op-payment-page'}>
      {!embedded ? (
        <header className="op-page-heading">
          <div>
            <h1>{c('支付订单')}</h1>
            <p>{c('按渠道事实核查到账和退款，每项操作保留原因与操作人。')}</p>
          </div>
        </header>
      ) : null}
      <form className="op-filters" onSubmit={search}>
        {textFilters.map(([key, label]) => (
          <Field key={key} label={label}>
            <input
              className="jm-field-input"
              value={draft[key]}
              onChange={(e) => setDraft({ ...draft, [key]: e.target.value })}
            />
          </Field>
        ))}
        <Field label={c('目标空间类型')}>
          <Choice
            label={c('目标空间类型')}
            value={draft.targetKind}
            onChange={(value) => setDraft({ ...draft, targetKind: value })}
            className="payment-choice"
            menuClassName="payment-choice-menu"
            options={[
              { value: '', label: c('全部空间') },
              { value: 'personal', label: c('个人空间') },
              { value: 'team', label: c('团队空间') },
            ]}
          />
        </Field>
        <Field label={c('支付渠道')}>
          <Choice
            label={c('支付渠道')}
            value={draft.channel}
            onChange={(value) => setDraft({ ...draft, channel: value })}
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
        <Field label={c('订单状态')}>
          <Choice
            label={c('订单状态')}
            value={draft.status}
            onChange={(value) => setDraft({ ...draft, status: value })}
            className="payment-choice"
            menuClassName="payment-choice-menu"
            options={[
              { value: '', label: c('全部状态') },
              ...(['pending', 'credited', 'closed', 'refunded'] as const).map((value) => ({
                value,
                label: paymentStatusLabel(value, c),
              })),
            ]}
          />
        </Field>
        <Field label={c('开始时间（北京时间）')}>
          <DateTimePicker
            label="开始时间（北京时间）"
            value={draft.start}
            onChange={(value) => setDraft({ ...draft, start: value })}
          />
        </Field>
        <Field label={c('结束时间（北京时间）')}>
          <DateTimePicker
            label="结束时间（北京时间）"
            value={draft.end}
            onChange={(value) => setDraft({ ...draft, end: value })}
          />
        </Field>
        <Button type="submit">{c('查询订单')}</Button>
        <Button
          variant="outline"
          onClick={() => {
            setDraft(emptyFilters);
            setFilters({});
            setPage(1);
          }}
        >
          {c('重置筛选')}
        </Button>
      </form>
      <PaymentMessages error={error || refunds.error} notice="" />
      <Feedback busy={read.busy} error={read.error} retry={read.refresh} />
      {read.value ? (
        <>
          {/* 列按内容自然宽度排开、不折行，窄于表格时在容器内横向滑动；操作列固定在右侧。 */}
          <div className="op-table-scroll op-payment-orders-scroll" tabIndex={0} aria-label={c('支付订单列表')}>
            <table className="op-table op-payment-orders">
              <thead>
                <tr>
                  {[
                    c('创建时间'),
                    c('订单号'),
                    c('下单人'),
                    c('目标空间'),
                    c('实付金额'),
                    c('充值 / 赠送积分'),
                    c('支付渠道'),
                    c('渠道交易号'),
                    c('状态'),
                    c('操作'),
                  ].map((v) => (
                    <th key={v}>{v}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {!read.value.items.length ? (
                  <tr>
                    <td colSpan={10} className="op-payment-empty" role="status">
                      {c('当前条件下暂无记录')}
                    </td>
                  </tr>
                ) : null}
                {read.value.items.map((o) => (
                  <tr key={o.orderNo}>
                    <td className="op-numeric">{dateTime(o.createdAt)}</td>
                    <td className="op-payment-mono">{o.orderNo}</td>
                    <td className="op-payment-id" title={o.buyerUserId}>
                      {o.buyerUserId}
                    </td>
                    <td className="op-payment-target">
                      {paymentTargetLabel(o.target, c)}
                      <small title={o.target.id}>{o.target.id}</small>
                    </td>
                    <td className="op-numeric">
                      <PaymentPrice amountFen={o.amountFen} originalAmountFen={o.originalAmountFen} />
                    </td>
                    <td className="op-numeric">
                      {formatInteger(String(o.rechargeCredits))} /{' '}
                      {formatInteger(String(o.giftCredits))}
                    </td>
                    <td>{paymentChannelLabel(o.channel, c)}</td>
                    <td className="op-payment-id" title={o.channelTradeNo || undefined}>
                      {o.channelTradeNo || '—'}
                    </td>
                    <td>
                      <span className="op-payment-status" data-status={o.status}>
                        {paymentStatusLabel(o.status, c)}
                      </span>
                    </td>
                    <td className="op-payment-actions-cell">
                      <Button size="sm" variant="outline" onClick={() => setSelected(o.orderNo)}>
                        {c('查看详情')}
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
            pageSize={pageSize}
            onSize={(v) => {
              setPageSize(v);
              setPage(1);
            }}
          />
        </>
      ) : null}
      <PaymentOrderDialog
        api={api}
        orderNo={selected}
        onClose={() => {
          setSelected('');
          read.refresh();
        }}
        refunds={refunds}
      />
    </section>
  );
}
export function PaymentOrderDialog({
  api,
  orderNo,
  onClose,
  refunds,
}: {
  api: PaymentAPI;
  orderNo: string;
  onClose: () => void;
  refunds: RefundDrafts;
}) {
  const { c } = useCatalogText();
  return (
    <Dialog
      open={!!orderNo}
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
    >
      <DialogContent className="op-payment-detail" variant="panel">
        <header className="op-payment-detail-head">
          <DialogTitle>{c('支付订单详情')}</DialogTitle>
          <DialogDescription>{c('金额、状态与账本以服务端最新记录为准。')}</DialogDescription>
        </header>
        {orderNo ? (
          <OrderDetails key={orderNo} api={api} orderNo={orderNo} refunds={refunds} />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
// 详情按“概要 → 订单信息 → 时间线 → 退款 → 积分流水 → 审计记录”分组成卡片，操作固定在底部。
function OrderDetails({
  api,
  orderNo,
  refunds,
}: {
  api: PaymentAPI;
  orderNo: string;
  refunds: RefundDrafts;
}) {
  const { c } = useCatalogText(),
    read = useRead((signal) => api.orderDetails(orderNo, signal), [api, orderNo]),
    action = usePaymentAction(),
    [reason, setReason] = useState(''),
    [queryOpen, setQueryOpen] = useState(false),
    [refundOpen, setRefundOpen] = useState(false);
  async function query(e: FormEvent) {
    e.preventDefault();
    if (!reason.trim()) return;
    await action.run(async (current) => {
      await api.queryOrder(orderNo, reason.trim());
      if (current()) {
        read.refresh();
        setQueryOpen(false);
        setReason('');
        action.setNotice(c('查单已完成，请核对最新记录。'));
      }
    });
  }
  const value = read.value,
    o = value?.order;
  const timeline = o
    ? ([
        [c('创建订单'), o.createdAt],
        [c('渠道支付'), o.paidAt],
        [c('积分到账'), o.creditedAt],
        [c('订单关闭'), o.closedAt],
        [c('退款完成'), o.refundedAt],
      ] as const)
    : [];
  return (
    <>
      <div className="op-payment-detail-body">
        <div className="op-payment-detail-id">
          <code>{orderNo}</code>
          {o ? (
            <span className="op-payment-status" data-status={o.status}>
              {paymentStatusLabel(o.status, c)}
            </span>
          ) : null}
        </div>
        <Feedback busy={read.busy} error={read.error} retry={read.refresh} />
        <PaymentMessages error={action.error} notice={action.notice} />
        {o && value ? (
          <>
            <dl className="op-payment-stats">
              <div>
                <dt>{c('实付金额')}</dt>
                <dd>
                  <PaymentPrice amountFen={o.amountFen} originalAmountFen={o.originalAmountFen} />
                </dd>
              </div>
              <div>
                <dt>{c('充值 / 赠送积分')}</dt>
                <dd>
                  {formatInteger(String(o.rechargeCredits))} / {formatInteger(String(o.giftCredits))}
                </dd>
              </div>
              <div>
                <dt>{c('支付渠道')}</dt>
                <dd>{paymentChannelLabel(o.channel, c)}</dd>
              </div>
              <div>
                <dt>{c('创建时间')}</dt>
                <dd>{dateTime(o.createdAt)}</dd>
              </div>
            </dl>
            <section className="op-payment-card">
              <h3>{c('订单信息')}</h3>
              <dl className="op-payment-info">
                <div>
                  <dt>{c('目标空间')}</dt>
                  <dd>
                    {paymentTargetLabel(o.target, c)}
                    <small>{o.target.id}</small>
                  </dd>
                </div>
                <div>
                  <dt>{c('下单人 ID')}</dt>
                  <dd>{o.buyerUserId}</dd>
                </div>
                <div>
                  <dt>{c('渠道交易号')}</dt>
                  <dd>{o.channelTradeNo || '—'}</dd>
                </div>
                <div>
                  <dt>{c('协议版本')}</dt>
                  <dd>{o.agreementVersion}</dd>
                </div>
                <div>
                  <dt>{c('有效期至')}</dt>
                  <dd>{dateTime(o.expiresAt)}</dd>
                </div>
                {o.closeReason ? (
                  <div>
                    <dt>{c('关闭原因')}</dt>
                    <dd>{o.closeReason}</dd>
                  </div>
                ) : null}
              </dl>
            </section>
            <section className="op-payment-card">
              <h3>{c('状态时间线')}</h3>
              <ol className="op-payment-timeline">
                {timeline.map(([label, at]) => (
                  <li key={label} data-done={!!at}>
                    <span>{label}</span>
                    <time dateTime={at || undefined}>{at ? dateTime(at) : c('未发生')}</time>
                  </li>
                ))}
              </ol>
            </section>
            <section className="op-payment-card">
              <h3>{c('退款历史')}</h3>
              {!value.refunds.length ? (
                <p className="op-muted">{c('暂无退款记录')}</p>
              ) : (
                <div className="op-table-scroll">
                  <table className="op-table op-payment-subtable">
                    <thead>
                      <tr>
                        {[
                          c('退款编号'),
                          c('类型 / 状态'),
                          c('退款金额'),
                          c('扣回充值 / 赠送积分'),
                          c('操作人'),
                          c('创建时间'),
                          c('退款完成时间'),
                          c('渠道退款号'),
                          c('跟进次数'),
                        ].map((v) => (
                          <th key={v}>{v}</th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {value.refunds.map((r) => (
                        <tr key={r.id}>
                          <td>
                            {r.refundNo}
                            <small>{r.reason}</small>
                          </td>
                          <td>
                            {paymentEventLabel(r.kind, c)} · {paymentEventLabel(r.status, c)}
                            {r.status === 'pending' ? (
                              <small>{c('退款结果尚未确认，积分恢复状态请核对审计记录。')}</small>
                            ) : null}
                          </td>
                          <td className="op-numeric">{formatPaymentAmount(r.amountFen)}</td>
                          <td className="op-numeric">
                            {r.rechargeCredits} / {r.giftCredits}
                          </td>
                          <td>{r.operatorId || '—'}</td>
                          <td className="op-numeric">{dateTime(r.createdAt)}</td>
                          <td className="op-numeric">{dateTime(r.finishedAt)}</td>
                          <td>{r.channelRefundId || '—'}</td>
                          <td className="op-numeric">{r.attempts}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
            <section className="op-payment-card">
              <h3>{c('关联积分流水')}</h3>
              <Ledger
                page={{
                  items: value.credits,
                  caption: '',
                  previous: null,
                  next: null,
                }}
                busy={false}
                onPage={() => {}}
              />
            </section>
            <section className="op-payment-card">
              <h3>{c('回调、查单与审计记录')}</h3>
              {!value.events.length ? (
                <p className="op-muted">{c('暂无事件记录')}</p>
              ) : (
                <div className="op-table-scroll">
                  <table className="op-table op-payment-subtable">
                    <thead>
                      <tr>
                        {[c('时间'), c('事件'), c('操作人'), c('原因 / 关联退款'), c('渠道事实')].map(
                          (v) => (
                            <th key={v}>{v}</th>
                          ),
                        )}
                      </tr>
                    </thead>
                    <tbody>
                      {value.events.map((event) => (
                        <tr key={event.id}>
                          <td className="op-numeric">{dateTime(event.createdAt)}</td>
                          <td>
                            {paymentEventLabel(event.source, c)} ·{' '}
                            {paymentEventLabel(event.outcome, c)}
                          </td>
                          <td>{event.operatorId || '—'}</td>
                          <td className="op-payment-reason-cell">
                            {event.reason || (event.refundId ? '' : '—')}
                            {event.refundId ? <small>{event.refundId}</small> : null}
                          </td>
                          <td>
                            {Object.keys(event.facts || {}).length ? (
                              <details className="op-payment-facts-toggle">
                                <summary>{c('查看')}</summary>
                                <PaymentFacts value={event.facts} label={c('已解析的渠道事实')} />
                              </details>
                            ) : (
                              '—'
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
          </>
        ) : null}
      </div>
      {o && value ? (
        <footer className="op-payment-detail-foot">
          <Button variant="outline" disabled={action.busy} onClick={read.refresh}>
            {c('刷新订单详情')}
          </Button>
          <Button variant="outline" disabled={action.busy} onClick={() => setQueryOpen(true)}>
            {c('重新查单')}
          </Button>
          {o.status === 'credited' && !value.refunds.some((r) => r.kind === 'operator') ? (
            <Button variant="destructive" onClick={() => setRefundOpen(true)}>
              {c('发起原路退款')}
            </Button>
          ) : null}
        </footer>
      ) : null}
      <Dialog open={queryOpen} onOpenChange={(v) => !action.busy && setQueryOpen(v)}>
        <DialogContent variant="form" className="op-dialog op-payment-dialog" closeDisabled={action.busy}>
          <DialogTitle>{c('重新查单')}</DialogTitle>
          <DialogDescription>
            {c('向支付渠道重新查询该订单，结果写入审计记录。')}
          </DialogDescription>
          <form className="op-payment-form" onSubmit={query}>
            <Field label={c('重新查单原因')}>
              <textarea
                className="jm-field-textarea"
                required
                maxLength={2000}
                value={reason}
                disabled={action.busy}
                onChange={(e) => setReason(e.target.value)}
              />
            </Field>
            <PaymentMessages error={action.error} notice="" />
            <div className="op-payment-buttons op-payment-buttons-end">
              <Button variant="outline" disabled={action.busy} onClick={() => setQueryOpen(false)}>
                {c('取消')}
              </Button>
              <Button type="submit" busy={action.busy} disabled={!reason.trim()}>
                {c('重新查单')}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
      <Dialog open={refundOpen} onOpenChange={setRefundOpen}>
        <DialogContent variant="form" className="op-dialog op-payment-dialog">
          <DialogTitle>{c('原路退款')}</DialogTitle>
          <DialogDescription>
            {c('先扣回积分，再向原支付渠道退款。每单最多一次运营退款。')}
          </DialogDescription>
          {refundOpen ? (
            <RefundForm
              api={api}
              orderNo={orderNo}
              drafts={refunds}
              onSaved={read.refresh}
              onClose={() => setRefundOpen(false)}
            />
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  );
}
function RefundForm({
  api,
  orderNo,
  drafts,
  onSaved,
  onClose,
}: {
  api: PaymentAPI;
  orderNo: string;
  drafts: RefundDrafts;
  onSaved: () => void;
  onClose: () => void;
}) {
  const { c } = useCatalogText(),
    action = usePaymentAction(),
    original = drafts.get(orderNo);
  const [credits, setCredits] = useState(original ? String(original.credits) : ''),
    [reason, setReason] = useState(original?.reason || ''),
    [command, setCommand] = useState<RefundCommand | null>(original || null),
    [quote, setQuote] = useState<PaymentRefundQuote | null>(null),
    [quotedCredits, setQuotedCredits] = useState(''),
    [confirm, setConfirm] = useState(false),
    [canRetry, setCanRetry] = useState(false);
  const maximum = useRead(
    (signal) => api.refundQuote(orderNo, 0, signal),
    [api, orderNo],
    !command,
  );
  const quoted = quote && quotedCredits === credits ? quote : null;
  async function calculate(e: FormEvent) {
    e.preventDefault();
    let amount: number;
    try {
      amount = paymentCreditsInput(credits);
      if (!reason.trim()) throw new Error(c('请填写退款原因。'));
    } catch (e) {
      action.setError((e as Error).message);
      return;
    }
    await action.run(async (current) => {
      const result = await api.refundQuote(orderNo, amount);
      if (current()) {
        setQuote(result);
        setQuotedCredits(credits);
        setConfirm(true);
      }
    });
  }
  async function send(input: RefundCommand) {
    await action.run(async (current) => {
      setCommand(input);
      drafts.set(orderNo, input);
      setCanRetry(false);
      try {
        await api.createRefund(orderNo, input);
        if (current()) {
          drafts.delete(orderNo);
          setCommand(null);
          setConfirm(false);
          onSaved();
          onClose();
        }
      } catch (e) {
        if (current()) {
          if (writeResultUnknown(e)) {
            setCommand(input);
            drafts.set(orderNo, input);
          } else {
            drafts.delete(orderNo);
            setCommand(null);
            setConfirm(false);
            setQuote(null);
            maximum.refresh();
          }
        }
        throw e;
      }
    });
  }
  async function reconcile() {
    await action.run(async (current) => {
      const result = await api.orderDetails(orderNo);
      if (!current() || !command) return;
      if (result.refunds.some((r) => r.id === command.refundId)) {
        drafts.delete(orderNo);
        setCommand(null);
        onSaved();
        onClose();
      } else if (result.refunds.some((r) => r.kind === 'operator')) {
        action.setNotice(c('本单已有运营退款，请核对退款历史。'));
        setCanRetry(false);
      } else {
        setCanRetry(true);
        action.setNotice(c('尚未查到该退款；如需重试，将使用原退款编号和原输入。'));
      }
    });
  }
  return (
    <>
      <Feedback busy={maximum.busy} error={maximum.error} retry={maximum.refresh} />
      {maximum.value ? (
        <p>
          {c('当前最多可退 {credits} 积分，折合 {amount}。', {
            credits: maximum.value.maxCredits,
            amount: formatPaymentAmount(maximum.value.amountFen),
          })}
        </p>
      ) : null}
      <form onSubmit={calculate} className="op-payment-form">
        <Field label={c('退款积分数')} hint={c('只能下调；最终上限会在提交事务中重新核验。')}>
          <input
            className="jm-field-input"
            inputMode="numeric"
            required
            value={credits}
            disabled={action.busy || !!command}
            onChange={(e) => {
              setCredits(e.target.value);
              setConfirm(false);
            }}
          />
        </Field>
        <Field label={c('退款原因')}>
          <textarea
            className="jm-field-textarea"
            required
            maxLength={2000}
            value={reason}
            disabled={action.busy || !!command}
            onChange={(e) => {
              setReason(e.target.value);
              setConfirm(false);
            }}
          />
        </Field>
        {!command ? (
          <Button
            type="submit"
            busy={action.busy}
            disabled={!maximum.value || !credits || !reason.trim()}
          >
            {c('计算并核对退款')}
          </Button>
        ) : null}
      </form>
      {command ? (
        <div className="op-pending">
          <p>{c('退款提交结果未确认。请先回读；不要创建新的退款编号。')}</p>
          <code>{command.refundId}</code>
          <div className="op-payment-buttons">
            <Button variant="outline" busy={action.busy} onClick={() => void reconcile()}>
              {c('回读退款结果')}
            </Button>
            {canRetry ? (
              <Button disabled={action.busy} onClick={() => void send(command)}>
                {c('按原编号重试')}
              </Button>
            ) : null}
          </div>
        </div>
      ) : null}
      <PaymentMessages error={action.error} notice={action.notice} />
      <Dialog open={confirm && !!quoted && !command} onOpenChange={setConfirm}>
        <DialogContent variant="confirm" className="op-dialog" closeDisabled={action.busy}>
          <DialogTitle>{c('确认原路退款')}</DialogTitle>
          <DialogDescription>
            {c('确认后将扣回积分并向支付渠道申请退款，金额以服务端重新核验为准。')}
          </DialogDescription>
          {quoted ? (
            <>
              <p>
                {c('退回 {amount}，扣回充值 {credits} 积分、赠送 {gift} 积分。', {
                  amount: formatPaymentAmount(quoted.amountFen),
                  credits: quoted.rechargeCredits,
                  gift: quoted.giftCredits,
                })}
              </p>
              <p className="op-payment-reason">{reason}</p>
              <div className="op-payment-buttons">
                <Button variant="outline" disabled={action.busy} onClick={() => setConfirm(false)}>
                  {c('返回修改')}
                </Button>
                <Button
                  variant="destructive"
                  busy={action.busy}
                  onClick={() =>
                    void send({
                      refundId: crypto.randomUUID(),
                      credits: quoted.rechargeCredits,
                      reason: reason.trim(),
                    })
                  }
                >
                  {c('确认退款')}
                </Button>
              </div>
            </>
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  );
}
