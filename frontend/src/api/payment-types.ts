import type { LedgerView } from './administration';

export type PaymentChannelID = 'demo' | 'wechatpay' | 'alipay';
export type PaymentStatus = 'pending' | 'credited' | 'closed' | 'refunded';
export type PaymentTarget = {
  kind: 'personal' | 'team';
  id: string;
  name: string;
};
export type PaymentPackage = {
  id: string;
  revision: number;
  amountFen: number;
  originalAmountFen: number;
  rechargeCredits: number;
  giftCredits: number;
  enabled: boolean;
  sort: number;
  testOnly: boolean;
  createdAt: string;
  updatedAt: string;
};
export type OperationsPaymentPackage = PaymentPackage & { name: string };
export type PaymentOrder = {
  orderNo: string;
  buyerUserId: string;
  clientRequestId: string;
  target: PaymentTarget;
  packageId: string;
  amountFen: number;
  originalAmountFen: number;
  rechargeCredits: number;
  giftCredits: number;
  channel: PaymentChannelID;
  codeUrl: string;
  channelTradeNo: string;
  status: PaymentStatus;
  closeReason?: string;
  agreementVersion: string;
  expiresAt: string;
  createdAt: string;
  creditedAt: string | null;
  paidAt: string | null;
  closedAt: string | null;
  refundedAt: string | null;
};
export type PaymentOptions = {
  channels: {
    id: PaymentChannelID;
    demo: boolean;
  }[];
  // 用户收银卡片展示档位名称；旧档位可能尚未命名（空字符串）。
  packages: (PaymentPackage & { name: string })[];
  targets: PaymentTarget[];
  agreement: {
    version: string;
    agreed: boolean;
  };
};
export type PaymentRefund = {
  id: string;
  refundNo: string;
  orderNo: string;
  kind: 'operator' | 'late_payment';
  amountFen: number;
  rechargeCredits: number;
  giftCredits: number;
  status: 'pending' | 'succeeded' | 'failed';
  operatorId?: string;
  reason: string;
  channelRefundId: string;
  attempts: number;
  billAt: string | null;
  createdAt: string;
  finishedAt: string | null;
};
export type PaymentRefundQuote = {
  maxCredits: number;
  rechargeCredits: number;
  giftCredits: number;
  amountFen: number;
};
export type PaymentEvent = {
  id: string;
  orderNo?: string;
  refundId?: string;
  source: string;
  outcome: string;
  operatorId?: string;
  reason?: string;
  facts: Record<string, unknown>;
  createdAt: string;
};
export type PaymentAnomaly = {
  id: string;
  orderNo?: string;
  channel: PaymentChannelID;
  kind: string;
  detail: Record<string, unknown>;
  status: 'open' | 'resolved';
  resolvedBy?: string;
  note?: string;
  resolvedAt: string | null;
  createdAt: string;
};
export type PaymentSettings = {
  revision: number;
  dailyLimitFen: number;
};
export type PaymentOrderDetails = {
  order: PaymentOrder;
  events: PaymentEvent[];
  refunds: PaymentRefund[];
  credits: LedgerView[];
};
export type PaymentPage<T> = {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
};
export type PaymentOrderFilters = Partial<{
  orderNo: string;
  channelTradeNo: string;
  buyerUserId: string;
  targetKind: PaymentTarget['kind'];
  targetId: string;
  channel: PaymentChannelID;
  status: PaymentStatus;
  start: string;
  end: string;
}>;
export type PaymentAnomalyFilters = Partial<{
  status: PaymentAnomaly['status'];
  kind: string;
  channel: PaymentChannelID;
}>;
export type PaymentCreateOrderInput = {
  clientRequestId: string;
  target: Pick<PaymentTarget, 'kind' | 'id'>;
  packageId: string;
  channel: PaymentChannelID;
  agreementVersion: string;
};
export type PaymentSavePackageInput = Omit<OperationsPaymentPackage, 'createdAt' | 'updatedAt'>;
export type PaymentAPI = {
  options(signal?: AbortSignal): Promise<PaymentOptions>;
  createOrder(input: PaymentCreateOrderInput): Promise<PaymentOrder>;
  getOrder(no: string, signal?: AbortSignal): Promise<PaymentOrder>;
  closeOrder(no: string): Promise<PaymentOrder>;
  orders(page?: number, pageSize?: number, signal?: AbortSignal): Promise<PaymentPage<PaymentOrder>>;
  simulate(no: string, result: 'paid' | 'failed'): Promise<PaymentOrder>;
  operationsOrders(filters?: PaymentOrderFilters, page?: number, pageSize?: number, signal?: AbortSignal): Promise<PaymentPage<PaymentOrder>>;
  orderDetails(no: string, signal?: AbortSignal): Promise<PaymentOrderDetails>;
  queryOrder(no: string, reason: string): Promise<PaymentOrder>;
  refundQuote(no: string, credits?: number, signal?: AbortSignal): Promise<PaymentRefundQuote>;
  createRefund(no: string, input: {
    refundId: string;
    credits: number;
    reason: string;
  }): Promise<PaymentRefund>;
  anomalies(filters?: PaymentAnomalyFilters, page?: number, pageSize?: number, signal?: AbortSignal): Promise<PaymentPage<PaymentAnomaly>>;
  resolveAnomaly(id: string, note: string): Promise<PaymentAnomaly>;
  packages(signal?: AbortSignal): Promise<OperationsPaymentPackage[]>;
  savePackage(input: PaymentSavePackageInput): Promise<OperationsPaymentPackage>;
  settings(signal?: AbortSignal): Promise<PaymentSettings>;
  saveSettings(input: PaymentSettings): Promise<PaymentSettings>;
};
