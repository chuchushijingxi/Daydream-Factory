import { useContext, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { sessionBoundary } from '../../api/session-boundary';
import type { PaymentPage } from '../../api/payment-types';
import type { CatalogText } from '../../contexts/useCatalogText';
import { useCatalogText } from '../../contexts/useCatalogText';
import { SessionOptionalContext } from '../../state/SessionContext';
import { Choice } from './controls';
import { Button } from '../../components/ui/button';
export function usePaymentEpoch() {
  return useSyncExternalStore(
    (listener) => sessionBoundary.subscribe(listener),
    () => sessionBoundary.epoch,
  );
}
export function usePaymentAction() {
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [notice, setNotice] = useState('');
  const active = useRef(true),
    guard = useRef(false);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  async function run(action: (current: () => boolean) => Promise<void>) {
    if (guard.current) return;
    guard.current = true;
    setBusy(true);
    setError('');
    setNotice('');
    const epoch = sessionBoundary.epoch,
      current = () => active.current && epoch === sessionBoundary.epoch;
    try {
      await action(current);
    } catch (e) {
      if (current()) setError((e as Error).message);
    } finally {
      guard.current = false;
      if (current()) setBusy(false);
    }
  }
  return { busy, error, notice, setError, setNotice, run };
}
export function PaymentMessages({ error, notice }: { error: string; notice: string }) {
  return (
    <>
      {error ? (
        <p className="op-inline-error" role="alert">
          {error}
        </p>
      ) : null}
      {notice ? (
        <p className="op-inline-note" role="status">
          {notice}
        </p>
      ) : null}
    </>
  );
}
export function PaymentPagination({
  page,
  busy,
  onPage,
  pageSize,
  onSize,
}: {
  page: PaymentPage<unknown>;
  busy: boolean;
  onPage: (v: number) => void;
  pageSize: number;
  onSize: (v: number) => void;
}) {
  const { c } = useCatalogText();
  return (
    <nav className="op-pagination" aria-label={c('分页')}>
      <div className="payment-page-size-field">
        {c('每页条数')}{' '}
        <Choice
          label={c('每页条数')}
          value={String(pageSize)}
          disabled={busy}
          className="payment-choice payment-page-size"
          menuClassName="payment-choice-menu"
          onChange={(value) => onSize(Number(value))}
          options={[10, 20, 50].map((value) => ({ value: String(value), label: String(value) }))}
        />
      </div>
      <span>
        {c('共 {total} 条，第 {page} 页', {
          total: page.total,
          page: page.page,
        })}
      </span>
      <Button
        size="sm"
        variant="ghost"
        disabled={busy || page.page <= 1}
        onClick={() => onPage(page.page - 1)}
      >
        {c('上一页')}
      </Button>
      <Button
        size="sm"
        variant="ghost"
        disabled={busy || page.page * page.pageSize >= page.total}
        onClick={() => onPage(page.page + 1)}
      >
        {c('下一页')}
      </Button>
    </nav>
  );
}
export function PaymentFacts({ value, label }: { value: Record<string, unknown>; label: string }) {
  return (
    <pre className="op-payment-facts" aria-label={label}>
      {JSON.stringify(value, null, 2)}
    </pre>
  );
}
export function paymentAnomalyLabel(kind: string, c: CatalogText) {
  const labels: Record<string, string> = {
    signature_invalid: c('通知验签失败'),
    notification_invalid: c('通知报文无效'),
    identity_mismatch: c('渠道身份不符'),
    amount_mismatch: c('支付金额不符'),
    paid_after_close: c('关单后付款'),
    bill_missing_local: c('账单存在而本地无记录'),
    bill_missing_channel: c('本地到账而账单无记录'),
    bill_amount_mismatch: c('账单金额不符'),
    refund_mismatch: c('退款事实不符'),
    refund_failed: c('退款失败或积分恢复受阻'),
  };
  return labels[kind] || kind;
}
export function paymentEventLabel(value: string, c: CatalogText) {
  const labels: Record<string, string> = {
    create: c('创建订单'),
    notify: c('渠道通知'),
    query: c('渠道查单'),
    close: c('关闭订单'),
    bill: c('账单核对'),
    simulate: c('演示模拟'),
    refund: c('支付退款'),
    pending: c('处理中'),
    succeeded: c('退款成功'),
    failed: c('操作失败'),
    created: c('已创建'),
    processing: c('处理中'),
    credited: c('已到账'),
    closed: c('已关闭'),
    refunded: c('已退款'),
    refund_accepted: c('退款已受理'),
    restore_pending: c('渠道已拒绝退款，积分恢复待处理'),
    paid: c('支付成功'),
    notpay: c('尚未支付'),
    not_found: c('渠道无记录'),
    late_payment: c('关单后付款退款'),
    operator: c('运营退款'),
  };
  return labels[value] || value;
}

export type PaymentRefundCommand = { refundId: string; credits: number; reason: string };
export type PaymentRefundDrafts = {
  get(orderNo: string): PaymentRefundCommand | undefined;
  set(orderNo: string, command: PaymentRefundCommand): void;
  delete(orderNo: string): void;
  error: string;
};
// 复用现有运营草稿前缀与账户清理规则：未知提交不能因刷新或切页换成新编号。
export function usePaymentRefundDrafts(): PaymentRefundDrafts {
  const session = useContext(SessionOptionalContext),
    { c } = useCatalogText();
  const storageKey = session?.user
    ? 'drama:credit-command:payment-refunds:' + session.user.identity
    : '';
  const [initial] = useState(() => {
    const commands = new Map<string, PaymentRefundCommand>();
    try {
      const saved = storageKey ? sessionStorage.getItem(storageKey) : null;
      if (saved) {
        const rows: unknown = JSON.parse(saved);
        if (!Array.isArray(rows)) throw new Error();
        for (const row of rows) {
          if (!Array.isArray(row) || row.length !== 2 || typeof row[0] !== 'string')
            throw new Error();
          const value = row[1];
          if (
            !value ||
            typeof value.refundId !== 'string' ||
            !value.refundId ||
            !Number.isSafeInteger(value.credits) ||
            value.credits <= 0 ||
            typeof value.reason !== 'string' ||
            !value.reason.trim()
          )
            throw new Error();
          commands.set(row[0], {
            refundId: value.refundId,
            credits: value.credits,
            reason: value.reason,
          });
        }
      }
      return { commands, failed: false };
    } catch {
      return { commands, failed: true };
    }
  });
  const [failed, setFailed] = useState(initial.failed);
  function persist() {
    if (!storageKey) return;
    try {
      if (initial.commands.size)
        sessionStorage.setItem(storageKey, JSON.stringify([...initial.commands]));
      else sessionStorage.removeItem(storageKey);
    } catch {
      setFailed(true);
    }
  }
  return {
    get: (no) => initial.commands.get(no),
    set: (no, command) => {
      initial.commands.set(no, command);
      persist();
    },
    delete: (no) => {
      initial.commands.delete(no);
      persist();
    },
    error: failed ? c('浏览器无法保存或读取退款编号，请保留本页并通过退款历史核对。') : '',
  };
}
