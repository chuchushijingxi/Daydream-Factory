import type {
  PaymentAPI,
  PaymentCreateOrderInput,
  PaymentOptions,
  PaymentOrder,
} from '../../api/payment-types';
import { ApiError } from '../../api/http-error';
import { translate } from '../../contexts/translate';

export type CheckoutAPI = Pick<
  PaymentAPI,
  'options' | 'createOrder' | 'getOrder' | 'closeOrder' | 'simulate'
>;
export type CheckoutSelection = {
  target: string;
  packageId: string;
  channel: PaymentCreateOrderInput['channel'] | '';
  agreed: boolean;
};
export type CheckoutState = {
  open: boolean;
  options: PaymentOptions | null;
  selection: CheckoutSelection;
  loading: boolean;
  busy: boolean;
  error: string;
  notice: string;
  order: PaymentOrder | null;
  uncertain: boolean;
  needsClose: boolean;
  // 收银页上的支付层：购买后显示，返回档位或重新打开时收起。
  checkout: boolean;
};
const emptySelection = (): CheckoutSelection => ({
  target: '',
  packageId: '',
  channel: '',
  agreed: false,
});
const failure = (error: unknown) =>
  error instanceof Error ? error.message : translate('支付操作失败，请稍后重试。');
// 这些错误无法证明未受理。保留原命令，只有用户显式操作才按原编号重试。
const uncertainFailure = (error: unknown) =>
  !(error instanceof ApiError) ||
  error.status === 0 ||
  error.status >= 500 ||
  error.code === 'PROGRAM_FAILED';

export class Checkout {
  private state: CheckoutState = {
    open: false,
    options: null,
    selection: emptySelection(),
    loading: false,
    busy: false,
    error: '',
    notice: '',
    order: null,
    uncertain: false,
    needsClose: false,
    checkout: false,
  };
  private listeners = new Set<() => void>();
  private generation = 0;
  private command: PaymentCreateOrderInput | null = null;
  private closeRequested = false;
  private pollSequence = 0;
  private writeSequence = 0;
  private optionsSequence = 0;
  private preferredTarget = '';
  constructor(
    readonly api: CheckoutAPI,
    private valid: () => boolean,
  ) {}
  snapshot = () => this.state;
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  private update(patch: Partial<CheckoutState>) {
    if (!this.valid()) return;
    this.state = { ...this.state, ...patch };
    this.listeners.forEach((listener) => listener());
  }
  invalidate() {
    ++this.generation;
    ++this.pollSequence;
    ++this.optionsSequence;
    this.state = {
      open: false,
      options: null,
      selection: emptySelection(),
      loading: false,
      busy: false,
      error: '',
      notice: '',
      order: null,
      uncertain: false,
      needsClose: false,
      checkout: false,
    };
    this.listeners.forEach((listener) => listener());
    this.listeners.clear();
  }
  async loadOptions() {
    const sequence = ++this.optionsSequence;
    this.update({ loading: true, error: '' });
    try {
      const options = await this.api.options();
      if (!this.valid() || sequence !== this.optionsSequence) return;
      const old = this.state.selection,
        preferred = this.preferredTarget;
      this.preferredTarget = '';
      this.update({
        options,
        selection: {
          target:
            [preferred, old.target].find(
              (value) => value && options.targets.some((t) => t.kind + ':' + t.id === value),
            ) ||
            (options.targets[0] ? options.targets[0].kind + ':' + options.targets[0].id : ''),
          packageId: options.packages.some((p) => p.id === old.packageId)
            ? old.packageId
            : options.packages[0]?.id || '',
          channel: options.channels.some((c) => c.id === old.channel)
            ? old.channel
            : options.channels[0]?.id || '',
          agreed:
            this.state.options?.agreement.version === options.agreement.version
              ? old.agreed
              : options.agreement.agreed,
        },
      });
    } catch (error) {
      if (sequence === this.optionsSequence) this.update({ error: failure(error) });
    } finally {
      if (sequence === this.optionsSequence) this.update({ loading: false });
    }
  }
  // target为打开入口所在空间（kind:id）；只在没有进行中订单时改选，不改动待支付单。
  open = (target?: string) => {
    if (!this.valid()) return;
    if (
      target &&
      this.state.order?.status !== 'pending' &&
      !this.state.uncertain &&
      !this.state.needsClose &&
      !this.state.busy
    ) {
      const options = this.state.options;
      if (options?.targets.some((t) => t.kind + ':' + t.id === target))
        this.update({ selection: { ...this.state.selection, target } });
      else {
        // 团队所有权或状态可能已变化：重新读取目标，读到后再采用。
        this.preferredTarget = target;
        if (options) void this.loadOptions();
      }
    }
    ++this.generation;
    // 未知命令或未关妥的旧单优先恢复，不能悄悄创建第二笔。
    this.update({
      open: true,
      error: this.state.uncertain || this.state.needsClose ? this.state.error : '',
      notice: '',
    });
    if (!this.state.options) void this.loadOptions();
    if (this.state.order && this.state.order.status !== 'pending' && !this.state.needsClose)
      this.update({ order: null });
    if (this.state.order?.status !== 'pending' && !this.state.uncertain && !this.state.needsClose)
      this.update({ checkout: false });
  };
  close = () => {
    if (!this.valid()) return;
    ++this.generation;
    ++this.pollSequence;
    this.closeRequested = true;
    this.update({ open: false });
    if (!this.state.busy && this.state.order?.status === 'pending') void this.closeKnownOrder();
  };
  agree = (agreed: boolean) => this.update({ selection: { ...this.state.selection, agreed } });
  // 点击档位“购买”即表示同意当前协议版本；下单仍提交该版本由服务端记录。
  async buy(packageId: string) {
    if (
      !this.valid() ||
      !this.state.open ||
      this.state.busy ||
      this.state.uncertain ||
      this.state.needsClose ||
      !this.state.options?.packages.some((p) => p.id === packageId)
    )
      return;
    const generation = this.generation;
    if (this.state.order?.status === 'pending' && !(await this.closeKnownOrder())) return;
    if (!this.valid() || generation !== this.generation || !this.state.open) return;
    this.update({
      selection: { ...this.state.selection, packageId, agreed: true },
      order: null,
      notice: '',
      error: '',
      checkout: true,
    });
    await this.create();
  }
  // 返回档位：待支付单先交服务端确认关闭；结果未知的下单命令须留在支付层核对。
  async back() {
    if (!this.valid() || this.state.busy || this.state.uncertain) return;
    if (this.state.order?.status === 'pending' && !(await this.closeKnownOrder())) return;
    if (!this.valid() || this.state.needsClose) return;
    this.update({ checkout: false, order: null, notice: '', error: '' });
  }
  async change(selection: Partial<CheckoutSelection>) {
    if (!this.valid() || this.state.busy || this.state.uncertain || this.state.needsClose)
      return;
    const generation = this.generation;
    const hadOrder = this.state.order?.status === 'pending';
    if (hadOrder && !(await this.closeKnownOrder())) return;
    if (!this.valid() || generation !== this.generation || !this.state.open) return;
    this.update({
      selection: { ...this.state.selection, ...selection },
      order: null,
      notice: '',
      error: '',
    });
    if (hadOrder) await this.create();
  }
  async create() {
    if (!this.valid() || !this.state.open || this.state.busy || this.state.needsClose) return;
    if (!this.command) {
      const { options, selection } = this.state;
      if (!options || !selection.agreed || !selection.channel) return;
      const target = options.targets.find((t) => t.kind + ':' + t.id === selection.target);
      if (!target || !options.packages.some((p) => p.id === selection.packageId)) return;
      this.command = {
        clientRequestId: crypto.randomUUID(),
        target: { kind: target.kind, id: target.id },
        packageId: selection.packageId,
        channel: selection.channel,
        agreementVersion: options.agreement.version,
      };
    }
    const command = this.command;
    ++this.writeSequence;
    const generation = this.generation;
    this.closeRequested = false;
    this.update({ busy: true, error: '', notice: '' });
    try {
      // 写请求不接受弹窗取消signal；迟到回执仍需取得订单号并关单。
      const order = await this.api.createOrder(command);
      if (!this.valid()) return;
      this.command = null;
      this.update({ order, uncertain: false });
      if (generation !== this.generation || !this.state.open || this.closeRequested) {
        this.update({ needsClose: order.status === 'pending', busy: false });
        if (order.status === 'pending') await this.closeKnownOrder();
      }
    } catch (error) {
      if (!this.valid()) return;
      const uncertain = uncertainFailure(error);
      if (!uncertain) this.command = null;
      this.update({ uncertain, error: failure(error) });
    } finally {
      this.update({ busy: false });
    }
  }
  async closeKnownOrder(): Promise<boolean> {
    const order = this.state.order;
    if (!this.valid() || !order || order.status !== 'pending') return true;
    ++this.writeSequence;
    ++this.pollSequence;
    this.update({ busy: true, error: '' });
    try {
      // 服务端唯一close用例执行先查后关；前端不调用真实渠道。
      const receipt = await this.api.closeOrder(order.orderNo);
      if (!this.valid()) return false;
      if (this.state.order?.orderNo === order.orderNo)
        this.update({
          order: receipt,
          needsClose: receipt.status === 'pending',
          error:
            receipt.status === 'pending' ? translate('旧订单尚未确认关闭，请稍后重试。') : '',
        });
      return receipt.status !== 'pending';
    } catch (error) {
      this.update({ needsClose: true, error: failure(error) });
      return false;
    } finally {
      this.update({ busy: false });
    }
  }
  async poll() {
    const order = this.state.order;
    if (
      !this.valid() ||
      !this.state.open ||
      this.state.busy ||
      this.state.needsClose ||
      order?.status !== 'pending'
    )
      return;
    const sequence = ++this.pollSequence,
      generation = this.generation;
    try {
      const receipt = await this.api.getOrder(order.orderNo);
      if (
        !this.valid() ||
        sequence !== this.pollSequence ||
        generation !== this.generation ||
        !this.state.open ||
        this.state.order?.orderNo !== order.orderNo
      )
        return;
      this.update({ order: receipt, error: '' });
    } catch (error) {
      if (sequence === this.pollSequence && generation === this.generation)
        this.update({ error: failure(error) });
    }
  }
  async simulate(result: 'paid' | 'failed') {
    const order = this.state.order;
    if (
      !this.valid() ||
      this.state.busy ||
      order?.channel !== 'demo' ||
      order.status !== 'pending'
    )
      return;
    const generation = this.generation;
    const write = ++this.writeSequence;
    // 到期关单或后续新下单会取得新的UI写归属；旧请求不得释放它的busy或覆盖错误。
    const ownsWrite = () => this.valid() && write === this.writeSequence;
    ++this.pollSequence;
    this.update({ busy: true, error: '', notice: '' });
    try {
      const receipt = await this.api.simulate(order.orderNo, result);
      if (!ownsWrite()) return;
      // 模拟失败可返回请求开始时的快照；同弹窗的到期关单可能已推进终态或新单。
      // 迟到回执只能投影到原来仍待支付的订单，不能把已确认事实退回pending。
      if (this.state.order?.orderNo !== order.orderNo || this.state.order.status !== 'pending')
        return;
      this.update({
        order: receipt,
        notice: result === 'failed' ? translate('模拟支付失败，可重试或关闭。') : '',
      });
      if (generation !== this.generation || !this.state.open) {
        this.update({ busy: false });
        if (receipt.status === 'pending') await this.closeKnownOrder();
      }
    } catch (error) {
      if (ownsWrite()) this.update({ error: failure(error) });
    } finally {
      if (ownsWrite()) {
        this.update({ busy: false });
        if (!this.state.open && this.state.order?.status === 'pending')
          await this.closeKnownOrder();
      }
    }
  }
}
