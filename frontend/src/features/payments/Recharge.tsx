import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from 'react';
import { Link } from 'react-router-dom';
import { CreditStar } from '../../components/ui/credit-star';
import { Button } from '../../components/ui/button';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { XIcon } from 'lucide-react';
import { PaymentPrice } from './PaymentPrice';
import { Spinner } from '../../components/ui/spinner';
import { paymentApi } from '../../api/payment-client';
import { sessionBoundary } from '../../api/session-boundary';
import { sessionAudience } from '../../api/session-audience';
import { SessionOptionalContext } from '../../state/SessionContext';
import { useOptionalSpace } from '../../state/SpaceContext';
import {
  formatPaymentAmount,
  paymentChannelLabel,
  paymentTargetLabel,
} from '../../domain/payment-format';
import type { PaymentOptions } from '../../api/payment-types';
import { formatInteger } from '../../pages/credits/credit-format';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Checkout, type CheckoutAPI } from './checkout';
import { PaymentQRCode } from './PaymentQRCode';
import { legalPaths } from '../../pages/legal/LegalDocumentPage';

type RechargeContextValue = {
  available: boolean;
  open(trigger?: HTMLElement, target?: string): void;
};
const Context = createContext<RechargeContextValue | null>(null);
// 只保留当前账号本页应用内的未确认写命令。路由重挂载不能丢掉原请求编号；
// 会话代次或平台变化即废弃，绝不携带新Cookie操作旧账户订单。
let retained: {
  api: CheckoutAPI;
  identity: string;
  service: unknown;
  epoch: number;
  controller: Checkout;
} | null = null;
function checkoutFor(api: CheckoutAPI, identity: string, service: unknown) {
  const epoch = sessionBoundary.epoch,
    audience = sessionAudience();
  if (
    retained?.api === api &&
    retained.identity === identity &&
    retained.service === service &&
    retained.epoch === epoch
  )
    return retained.controller;
  // 新scope替换即令旧控制器失效；不在React渲染期间通知旧订阅者。
  let controller: Checkout;
  controller = new Checkout(
    api,
    () =>
      retained?.controller === controller &&
      epoch === sessionBoundary.epoch &&
      audience === sessionAudience(),
  );
  retained = { api, identity, service, epoch, controller };
  return controller;
}
sessionBoundary.subscribe(() => {
  retained?.controller.invalidate();
  retained = null;
});

export function RechargeProvider({
  children,
  api = paymentApi,
}: {
  children: ReactNode;
  api?: CheckoutAPI;
}) {
  const session = useContext(SessionOptionalContext);
  const returnFocus = useRef<HTMLElement | null>(null);
  const controller = useMemo(
    () => checkoutFor(api, session?.user?.identity || '', session?.service),
    [api, session?.user?.identity, session?.service, session?.epoch],
  );
  const state = useSyncExternalStore(
    controller.subscribe,
    controller.snapshot,
    controller.snapshot,
  );
  useEffect(() => {
    if (session?.user) void controller.loadOptions();
    return () => controller.close();
  }, [controller, session?.user?.identity]);
  const available =
    !!session?.user && (state.options ? !!state.options.channels.length : !!state.error);
  return (
    <Context.Provider
      value={{
        available,
        open: (trigger, target) => {
          returnFocus.current = trigger || null;
          controller.open(target);
        },
      }}
    >
      {children}
      <RechargeDialog controller={controller} returnFocus={returnFocus} />
    </Context.Provider>
  );
}
export const useRecharge = () => useContext(Context);
export function RechargeButton({
  className,
  onClick,
}: {
  className?: string;
  onClick?: () => void;
}) {
  const { c } = useCatalogText(),
    recharge = useRecharge(),
    current = useOptionalSpace()?.current;
  if (!recharge?.available) return null;
  return (
    <Button
      className={className}
      size="sm"
      onClick={(event) => {
        const trigger = event.currentTarget.closest('.personal-credit-panel')
          ? document.querySelector<HTMLElement>('.credit-balance-trigger')
          : event.currentTarget;
        onClick?.();
        // 默认选中入口所在空间；个人空间 ID 即用户 ID。
        recharge.open(trigger || undefined, current ? current.kind + ':' + current.id : undefined);
      }}
    >
      {c('充值')}
    </Button>
  );
}
function Countdown({
  expiresAt,
  onExpire,
  demo = false,
}: {
  expiresAt: string;
  onExpire: () => void;
  demo?: boolean;
}) {
  const { c } = useCatalogText();
  const [now, setNow] = useState(Date.now);
  const seconds = Math.max(0, Math.ceil((Date.parse(expiresAt) - now) / 1000));
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [expiresAt]);
  useEffect(() => {
    if (seconds === 0) onExpire();
  }, [seconds]);
  const time =
    Math.floor(seconds / 60)
      .toString()
      .padStart(2, '0') +
    ':' +
    (seconds % 60).toString().padStart(2, '0');
  return (
    <p className="payment-countdown">
      {seconds
        ? demo
          ? c('订单剩余有效时间 {time}', { time })
          : c('二维码剩余有效时间 {time}', { time })
        : c('订单已到期，正在确认支付并关闭…')}
    </p>
  );
}
type PaymentPackageOption = PaymentOptions['packages'][number];
// 角标只由档位自身的真实积分和双价计算，不补造限购、有效期或可生成时长。
function PackageCard({
  item,
  disabled,
  busy,
  onBuy,
}: {
  item: PaymentPackageOption;
  disabled: boolean;
  busy: boolean;
  onBuy: () => void;
}) {
  const { c } = useCatalogText();
  const giftPercent =
    item.giftCredits > 0 ? Math.floor((item.giftCredits * 100) / item.rechargeCredits) : 0;
  const saved = item.originalAmountFen - item.amountFen;
  return (
    <li className="payment-package-card">
      <header className="payment-package-head">
        {item.name ? <h3 title={item.name}>{item.name}</h3> : null}
        <div className="payment-package-badges">
        {giftPercent > 0 ? (
          <span data-tone="gift">{c('加赠{percent}%', { percent: giftPercent })}</span>
        ) : null}
        {saved > 0 ? (
          <span>{c('立省{amount}', { amount: formatPaymentAmount(saved) })}</span>
        ) : null}
        {item.testOnly ? <span data-tone="test">{c('测试档位 · 仅运营可见')}</span> : null}
        </div>
      </header>
      <p className="payment-package-total">
        <CreditStar />
        <strong>{formatInteger(String(item.rechargeCredits + item.giftCredits))}</strong>
        <span>{c('积分')}</span>
      </p>
      <p className="payment-package-split">
        {item.giftCredits > 0
          ? c('充值 {recharge} + 赠送 {gift}', {
              recharge: formatInteger(String(item.rechargeCredits)),
              gift: formatInteger(String(item.giftCredits)),
            })
          : c('充值 {recharge}', { recharge: formatInteger(String(item.rechargeCredits)) })}
      </p>
      <div className="payment-package-foot">
        <PaymentPrice amountFen={item.amountFen} originalAmountFen={item.originalAmountFen} />
        <Button className="payment-package-buy" disabled={disabled} busy={busy} onClick={onBuy}>
          {c('购买')}
        </Button>
      </div>
    </li>
  );
}
function RechargeDialog({
  controller,
  returnFocus,
}: {
  controller: Checkout;
  returnFocus: React.MutableRefObject<HTMLElement | null>;
}) {
  const { c } = useCatalogText();
  const state = useSyncExternalStore(
    controller.subscribe,
    controller.snapshot,
    controller.snapshot,
  );
  const { options, selection, order } = state;
  useEffect(() => {
    if (!state.open || order?.status !== 'pending' || state.needsClose) return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      await controller.poll();
      if (!stopped) timer = setTimeout(poll, 2000);
    };
    timer = setTimeout(poll, 2000);
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [controller, state.open, order?.orderNo, order?.status, state.needsClose]);
  const locked = state.busy || state.uncertain || state.needsClose;
  const paying =
    state.checkout || order?.status === 'pending' || state.uncertain || state.needsClose;
  const selectedTarget = options?.targets.find((t) => t.kind + ':' + t.id === selection.target);
  const ready = !!options && !!options.channels.length && !!options.packages.length && !!options.targets.length;
  return (
    <DialogPrimitive.Root
      open={state.open}
      onOpenChange={(open) => {
        if (!open) controller.close();
      }}
    >
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="payment-sheet-mask" />
        <DialogPrimitive.Content
          className="payment-sheet"
          aria-describedby={undefined}
          onOpenAutoFocus={(event) => {
            // 打开时聚焦收银页本身，不让目标选择框带着焦点环出现；Tab 仍按顺序进入控件。
            event.preventDefault();
            (event.currentTarget as HTMLElement | null)?.focus({ preventScroll: true });
          }}
          onEscapeKeyDown={(event) => {
            // Escape 先从支付层回到档位，再关闭整个收银页。
            if (paying) {
              event.preventDefault();
              void controller.back();
            }
          }}
          onCloseAutoFocus={(event) => {
            if (returnFocus.current?.isConnected) {
              event.preventDefault();
              returnFocus.current.focus({ preventScroll: true });
            }
          }}
        >
          <header className="payment-sheet-bar">
            <DialogPrimitive.Title className="payment-sheet-title">
              <CreditStar />
              {c('积分充值')}
            </DialogPrimitive.Title>
            <DialogPrimitive.Close asChild>
              <Button variant="ghost" className="payment-sheet-close" aria-label={c('关闭')}>
                <XIcon aria-hidden />
              </Button>
            </DialogPrimitive.Close>
          </header>
          <div
            className="payment-sheet-body"
            // React 18 类型尚无 inert；支付层打开时档位不可聚焦、不可点击。
            {...((paying ? { inert: '' } : {}) as Record<string, string>)}
          >
            <div className="payment-sheet-head">
              <h2>{c('选择充值档位')}</h2>
              <p>
                {selectedTarget
                  ? c('支付成功后积分到账至{target}。', {
                      target: paymentTargetLabel(selectedTarget, c),
                    })
                  : c('支付成功后积分到账至所选空间。')}
              </p>
              {options && options.targets.length > 1 ? (
                // 多个可充值空间时用居中分段切换（参考小云雀分组页签），单个空间只在说明里写明。
                <div className="payment-target-switch" role="radiogroup" aria-label={c('充值目标')}>
                  {options.targets.map((target) => {
                    const value = target.kind + ':' + target.id;
                    return (
                      <button
                        key={value}
                        type="button"
                        role="radio"
                        aria-checked={value === selection.target}
                        disabled={locked || paying}
                        onClick={() => void controller.change({ target: value })}
                      >
                        {paymentTargetLabel(target, c)}
                      </button>
                    );
                  })}
                </div>
              ) : null}
            </div>
            {state.loading && !options ? (
              <div className="payment-feedback" role="status">
                <Spinner />
                {c('读取充值选项…')}
              </div>
            ) : null}
            {state.error && !paying ? (
              <div className="payment-error" role="alert">
                <p>{state.error}</p>
                {!options ? (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => void controller.loadOptions()}
                    busy={state.loading}
                  >
                    {c('重新读取')}
                  </Button>
                ) : null}
              </div>
            ) : null}
            {options && !options.channels.length ? (
              <p className="payment-feedback">{c('暂未开放充值渠道。')}</p>
            ) : options && (!options.packages.length || !options.targets.length) ? (
              <p className="payment-feedback">{c('暂无可用充值档位或目标空间，请稍后再试。')}</p>
            ) : null}
            {ready && options ? (
              <>
                <ul className="payment-package-grid" aria-label={c('充值档位')}>
                  {options.packages.map((item) => (
                    <PackageCard
                      key={item.id}
                      item={item}
                      disabled={locked || !selection.target || !selection.channel}
                      busy={state.busy && selection.packageId === item.id && !order}
                      onBuy={() => void controller.buy(item.id)}
                    />
                  ))}
                </ul>
                <footer className="payment-sheet-terms">
                  <p>
                    {c('购买即代表同意')}
                    <Link to={legalPaths.recharge} target="_blank" rel="noopener">
                      {c('《积分充值协议》')}
                    </Link>
                    <small>{c('协议版本 {version}', { version: options.agreement.version })}</small>
                  </p>
                  <p>
                    {c('积分为平台内虚拟服务额度，不可提现或转让；充值后原则上不予退款，需要发票请联系客服。')}
                  </p>
                </footer>
              </>
            ) : null}
          </div>
          {paying && options ? (
            <CheckoutLayer controller={controller} />
          ) : null}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
function CheckoutLayer({ controller }: { controller: Checkout }) {
  const { c } = useCatalogText();
  const state = useSyncExternalStore(
    controller.subscribe,
    controller.snapshot,
    controller.snapshot,
  );
  const { options, selection, order } = state;
  const card = useRef<HTMLElement>(null);
  useEffect(() => {
    card.current?.focus({ preventScroll: true });
  }, []);
  if (!options) return null;
  const chosen = options.packages.find((p) => p.id === selection.packageId);
  const target = options.targets.find((t) => t.kind + ':' + t.id === selection.target);
  const successful = order?.status === 'credited' || order?.status === 'refunded';
  const recharge = order?.rechargeCredits ?? chosen?.rechargeCredits;
  const gift = order?.giftCredits ?? chosen?.giftCredits ?? 0;
  return (
    <div className="payment-checkout-layer">
      <section
        ref={card}
        tabIndex={-1}
        className="payment-checkout-card"
        aria-label={c('支付收银台')}
      >
        <Button
          variant="ghost"
          className="jm-button--close payment-checkout-close"
          aria-label={c('关闭支付')}
          disabled={state.busy || state.uncertain}
          onClick={() => void controller.back()}
        >
          <XIcon aria-hidden />
        </Button>
        {successful && order ? (
          <div className="payment-success" role="status">
            <CreditStar />
            <h3>
              {c('已到账 {credits} 积分到{target}', {
                credits: formatInteger(String(order.rechargeCredits)),
                target: paymentTargetLabel(order.target, c),
              })}
            </h3>
            {order.giftCredits > 0 ? (
              <p>
                {c('另赠 {credits} 积分', { credits: formatInteger(String(order.giftCredits)) })}
              </p>
            ) : null}
            {order.status === 'refunded' ? (
              <p>{c('此订单已有支付退款，实际积分以积分明细为准。')}</p>
            ) : null}
            <p>
              {c('订单号')} {order.orderNo}
            </p>
            <Link to="/credits" onClick={controller.close}>
              {c('查看积分明细')}
            </Link>
          </div>
        ) : (
          <>
            <h3 className="payment-checkout-title">{c('确认支付')}</h3>
            <dl className="payment-checkout-summary">
              <div>
                <dt>{c('充值到')}</dt>
                <dd>{order ? paymentTargetLabel(order.target, c) : target ? paymentTargetLabel(target, c) : '—'}</dd>
              </div>
              <div>
                <dt>{c('获得积分')}</dt>
                <dd>
                  {recharge === undefined
                    ? '—'
                    : gift > 0
                      ? c('充值 {recharge} + 赠送 {gift}', {
                          recharge: formatInteger(String(recharge)),
                          gift: formatInteger(String(gift)),
                        })
                      : c('充值 {recharge}', { recharge: formatInteger(String(recharge)) })}
                </dd>
              </div>
              <div className="payment-payable">
                <dt>{c('实付金额')}</dt>
                <dd>
                  {order ? (
                    <PaymentPrice amountFen={order.amountFen} originalAmountFen={order.originalAmountFen} />
                  ) : chosen ? (
                    <PaymentPrice amountFen={chosen.amountFen} originalAmountFen={chosen.originalAmountFen} />
                  ) : (
                    '—'
                  )}
                </dd>
              </div>
            </dl>
            {options.channels.length > 1 ? (
              <fieldset className="payment-channel-switch" disabled={state.busy || state.uncertain || state.needsClose}>
                <legend>{c('支付渠道')}</legend>
                <div className="payment-channels">
                  {options.channels.map((channel) => (
                    <label data-selected={channel.id === selection.channel} key={channel.id}>
                      <input
                        type="radio"
                        name="payment-channel"
                        value={channel.id}
                        checked={channel.id === selection.channel}
                        onChange={() => void controller.change({ channel: channel.id })}
                      />
                      {paymentChannelLabel(channel.id, c)}
                    </label>
                  ))}
                </div>
              </fieldset>
            ) : null}
            {state.error ? (
              <div className="payment-error" role="alert">
                <p>{state.error}</p>
              </div>
            ) : null}
            {state.uncertain ? (
              <div className="payment-notice" role="status">
                <p>{c('下单结果尚未确认。请使用原请求编号重试核对，勿重复付款。')}</p>
                <Button variant="outline" size="sm" busy={state.busy} onClick={() => void controller.create()}>
                  {c('原编号重试核对')}
                </Button>
              </div>
            ) : null}
            {state.needsClose ? (
              <div className="payment-notice" role="status">
                <p>{c('旧订单尚未确认关闭，请重试关单后继续。')}</p>
                <Button variant="outline" size="sm" busy={state.busy} onClick={() => void controller.closeKnownOrder()}>
                  {c('重新确认并关闭订单')}
                </Button>
              </div>
            ) : null}
            {!order && state.busy ? (
              <div className="payment-feedback" role="status">
                <Spinner />
                {c('正在下单…')}
              </div>
            ) : null}
            {order?.status === 'pending' ? (
              <section className="payment-cashier" aria-label={c('支付信息')}>
                {order.channel === 'demo' ? (
                  <>
                    <div className="payment-demo-actions">
                      <Button busy={state.busy} disabled={state.needsClose} onClick={() => void controller.simulate('paid')}>
                        {c('模拟支付成功')}
                      </Button>
                      <Button
                        variant="outline"
                        disabled={state.busy || state.needsClose}
                        onClick={() => void controller.simulate('failed')}
                      >
                        {c('模拟失败')}
                      </Button>
                    </div>
                  </>
                ) : order.codeUrl ? (
                  <>
                    <PaymentQRCode value={order.codeUrl} />
                    <p>
                      {c('请使用{channel}扫描二维码完成付款。', {
                        channel: paymentChannelLabel(order.channel, c),
                      })}
                    </p>
                  </>
                ) : (
                  <p role="status">{c('收银信息尚未确认，请稍后查看订单状态或关闭重试。')}</p>
                )}
                {state.notice ? <p role="status">{state.notice}</p> : null}
                <Countdown
                  key={order.orderNo}
                  demo={order.channel === 'demo'}
                  expiresAt={order.expiresAt}
                  onExpire={() => {
                    if (!controller.snapshot().needsClose) void controller.closeKnownOrder();
                  }}
                />
                <p className="payment-secondary">
                  {c('支付后关闭不影响到账；关闭或更换渠道会先取消当前订单。')}
                </p>
                <p className="payment-order-number">
                  {c('订单号')} <span>{order.orderNo}</span>
                </p>
              </section>
            ) : null}
            {order?.status === 'closed' ? (
              <div className="payment-notice" role="status">
                <p>{c('订单已关闭，继续充值请重新下单。')}</p>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={state.busy || state.needsClose}
                  onClick={() => chosen && void controller.buy(chosen.id)}
                >
                  {c('重新下单')}
                </Button>
              </div>
            ) : null}
          </>
        )}
      </section>
    </div>
  );
}
