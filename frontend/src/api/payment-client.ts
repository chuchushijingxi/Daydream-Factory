import { createHttpClient, jsonPost, ApiError } from './client';
import { creditLedgerView } from './credit-client';
import type { CreditReceiptDTO } from './administration-types';
import { translate } from '../contexts/translate';
import type {
  PaymentAPI,
  PaymentChannelID,
  PaymentStatus,
  PaymentTarget,
  PaymentPackage,
  OperationsPaymentPackage,
  PaymentOrder,
  PaymentOptions,
  PaymentRefund,
  PaymentRefundQuote,
  PaymentEvent,
  PaymentAnomaly,
  PaymentSettings,
  PaymentOrderDetails,
  PaymentPage,
  PaymentOrderFilters,
  PaymentAnomalyFilters,
} from './payment-types';
export type * from './payment-types';

function malformed(): never {
  throw new ApiError(200, 'PROGRAM_FAILED', translate('支付回执与模块合同不一致，请重新读取核对。'));
}
function invalid(): never {
  throw new ApiError(400, 'PAYMENT_INPUT_INVALID', translate('请核对支付请求中的编号、金额和操作原因。'));
}
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) malformed();
  return value as Record<string, unknown>;
}
function text(value: unknown, empty = false): string {
  if (typeof value !== 'string' || (!empty && !value)) malformed();
  return value;
}
function optionalText(value: unknown) { return value === undefined ? undefined : text(value); }
function integer(value: unknown, positive = false): number {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < (positive ? 1 : 0)) malformed();
  return value;
}
function signedInteger(value: unknown): number {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < -2147483648 || value > 2147483647) malformed();
  return value;
}
function boolean(value: unknown): boolean {
  if (typeof value !== 'boolean') malformed();
  return value;
}
function at(value: unknown): string {
  const out = text(value);
  if (!Number.isFinite(Date.parse(out))) malformed();
  return out;
}
function nullableAt(value: unknown) { return value === null ? null : at(value); }
function oneOf<T extends string>(value: unknown, choices: readonly T[]): T {
  if (typeof value !== 'string' || !choices.includes(value as T)) malformed();
  return value as T;
}
const channel = (value: unknown): PaymentChannelID => oneOf(value, ['demo', 'wechatpay', 'alipay']);
const status = (value: unknown): PaymentStatus => oneOf(value, ['pending', 'credited', 'closed', 'refunded']);
function array<T>(value: unknown, parse: (row: unknown) => T): T[] {
  if (!Array.isArray(value)) malformed();
  return value.map(parse);
}
function unique<T>(items: T[], key: (item: T) => string): T[] {
  if (new Set(items.map(key)).size !== items.length) malformed();
  return items;
}
function target(value: unknown): PaymentTarget {
  const v = object(value);
  return { kind: oneOf(v.kind, ['personal', 'team']), id: text(v.id), name: text(v.name) };
}
function snapshot(v: Record<string, unknown>) {
  const out = {
    amountFen: integer(v.amountFen, true),
    originalAmountFen: integer(v.originalAmountFen, true),
    rechargeCredits: integer(v.rechargeCredits, true),
    giftCredits: integer(v.giftCredits)
  };
  if (out.originalAmountFen < out.amountFen) malformed();
  if (BigInt(out.rechargeCredits) + BigInt(out.giftCredits) > BigInt(Number.MAX_SAFE_INTEGER)) malformed();
  return out;
}
function paymentPackage(value: unknown): PaymentPackage {
  const v = object(value);
  return {
    id: text(v.id),
    revision: integer(v.revision),
    ...snapshot(v),
    enabled: boolean(v.enabled),
    sort: signedInteger(v.sort),
    testOnly: boolean(v.testOnly),
    createdAt: at(v.createdAt),
    updatedAt: at(v.updatedAt)
  };
}
function validPackageName(value: string) {
  return value === value.trim() && Array.from(value).length >= 1 && Array.from(value).length <= 48 && !/[\p{Cc}\uD800-\uDFFF]/u.test(value);
}
function namedPaymentPackage(value: unknown): OperationsPaymentPackage {
  const v = object(value), name = text(v.name, true);
  // 迁移行允许待命名；新写入和保存回执始终核对有效的运营名称。
  if (name && !validPackageName(name)) malformed();
  return { ...paymentPackage(v), name };
}
function inputPackageName(value: string) {
  if (typeof value !== 'string') invalid();
  const name = value.trim();
  if (!validPackageName(name)) invalid();
  return name;
}
function order(value: unknown): PaymentOrder {
  const v = object(value);
  const out: PaymentOrder = {
    orderNo: text(v.orderNo),
    buyerUserId: text(v.buyerUserId),
    clientRequestId: text(v.clientRequestId),
    target: target(v.target),
    packageId: text(v.packageId),
    ...snapshot(v),
    channel: channel(v.channel),
    codeUrl: text(v.codeUrl, true),
    channelTradeNo: text(v.channelTradeNo, true),
    status: status(v.status),
    closeReason: optionalText(v.closeReason),
    agreementVersion: text(v.agreementVersion),
    expiresAt: at(v.expiresAt),
    createdAt: at(v.createdAt),
    creditedAt: nullableAt(v.creditedAt),
    paidAt: nullableAt(v.paidAt),
    closedAt: nullableAt(v.closedAt),
    refundedAt: nullableAt(v.refundedAt),
  };
  if ((out.status === 'credited' || out.status === 'refunded') &&
      (!out.creditedAt || !out.paidAt || !out.channelTradeNo)) malformed();
  if (out.status === 'closed' && !out.closedAt) malformed();
  if (out.status === 'refunded' && !out.refundedAt) malformed();
  return out;
}
function options(value: unknown): PaymentOptions {
  const v = object(value), agreement = object(v.agreement);
  return {
    channels: unique(array(v.channels, raw => {
      const ch = object(raw), id = channel(ch.id), demo = boolean(ch.demo);
      if (demo !== (id === 'demo')) malformed();
      return { id, demo };
    }), ch => ch.id),
    packages: unique(array(v.packages, namedPaymentPackage), p => p.id),
    targets: unique(array(v.targets, target), t => t.kind + ':' + t.id),
    agreement: { version: text(agreement.version), agreed: boolean(agreement.agreed) },
  };
}
function refund(value: unknown): PaymentRefund {
  const v = object(value);
  const out: PaymentRefund = {
    id: text(v.id),
    refundNo: text(v.refundNo),
    orderNo: text(v.orderNo),
    kind: oneOf(v.kind, ['operator', 'late_payment']),
    amountFen: integer(v.amountFen, true),
    rechargeCredits: integer(v.rechargeCredits),
    giftCredits: integer(v.giftCredits),
    status: oneOf(v.status, ['pending', 'succeeded', 'failed']),
    operatorId: optionalText(v.operatorId),
    reason: text(v.reason),
    channelRefundId: text(v.channelRefundId, true),
    attempts: integer(v.attempts),
    billAt: nullableAt(v.billAt),
    createdAt: at(v.createdAt),
    finishedAt: nullableAt(v.finishedAt),
  };
  if (out.status !== 'pending' && !out.finishedAt) malformed();
  if (out.status === 'succeeded' && !out.billAt) malformed();
  return out;
}
function quote(value: unknown): PaymentRefundQuote {
  const v = object(value), out = {
    maxCredits: integer(v.maxCredits),
    rechargeCredits: integer(v.rechargeCredits),
    giftCredits: integer(v.giftCredits),
    amountFen: integer(v.amountFen)
  };
  if (out.rechargeCredits > out.maxCredits) malformed();
  return out;
}
function event(value: unknown): PaymentEvent {
  const v = object(value);
  return {
    id: text(v.id),
    orderNo: optionalText(v.orderNo),
    refundId: optionalText(v.refundId),
    source: text(v.source),
    outcome: text(v.outcome),
    operatorId: optionalText(v.operatorId),
    reason: optionalText(v.reason),
    facts: object(v.facts),
    createdAt: at(v.createdAt)
  };
}
function anomaly(value: unknown): PaymentAnomaly {
  const v = object(value);
  return {
    id: text(v.id),
    orderNo: optionalText(v.orderNo),
    channel: channel(v.channel),
    kind: text(v.kind),
    detail: object(v.detail),
    status: oneOf(v.status, ['open', 'resolved']),
    resolvedBy: optionalText(v.resolvedBy),
    note: optionalText(v.note),
    resolvedAt: nullableAt(v.resolvedAt),
    createdAt: at(v.createdAt)
  };
}
function settings(value: unknown): PaymentSettings {
  const v = object(value);
  return { revision: integer(v.revision), dailyLimitFen: integer(v.dailyLimitFen) };
}
function page<T>(value: unknown, parse: (row: unknown) => T, expectedPage: number, expectedSize: number): PaymentPage<T> {
  const v = object(value), out = {
    items: array(v.items, parse),
    total: integer(v.total),
    page: integer(v.page, true),
    pageSize: integer(v.pageSize, true)
  };
  if (out.page !== expectedPage || out.pageSize !== expectedSize || out.pageSize > 100 || out.items.length > out.pageSize || out.items.length > out.total) malformed();
  return out;
}
function details(value: unknown, no: string): PaymentOrderDetails {
  const v = object(value), out = {
    order: matchingOrder(v.order, no),
    events: array(v.events, event),
    refunds: array(v.refunds, refund),
    credits: array(v.credits, raw => creditLedgerView(raw as CreditReceiptDTO))
  };
  if (out.events.some(e => e.orderNo && e.orderNo !== no) || out.refunds.some(r => r.orderNo !== no) || out.credits.some(c => c.source !== 'payment-order:' + no)) malformed();
  return out;
}
function matchingOrder(value: unknown, no: string) {
  const out = order(value);
  if (out.orderNo !== no) malformed();
  return out;
}
function inputID(value: string) {
  if (typeof value !== 'string' || !value || value.trim() !== value || new TextEncoder().encode(value).length > 128 || /[\u0000\r\n]/u.test(value)) invalid();
  return value;
}
function inputInteger(value: number, positive = false) {
  if (!Number.isSafeInteger(value) || value < (positive ? 1 : 0)) invalid();
  return value;
}
function reason(value: string) {
  if (typeof value !== 'string' || !value.trim() || new TextEncoder().encode(value).length > 2000 || value.includes('\0')) invalid();
  return value;
}
function inputChannel(value: string): PaymentChannelID {
  if (!['demo', 'wechatpay', 'alipay'].includes(value)) invalid();
  return value as PaymentChannelID;
}
function inputTarget(value: {
  kind: string;
  id: string;
}) {
  if (!value || !['personal', 'team'].includes(value.kind)) invalid();
  return { kind: value.kind as PaymentTarget['kind'], id: inputID(value.id) };
}
function pagination(p = 1, size = 20) {
  inputInteger(p, true);
  inputInteger(size, true);
  if (size > 100 || p > 1000000) invalid();
  return new URLSearchParams({ page: String(p), pageSize: String(size) });
}
function filterQuery(filters: PaymentOrderFilters | PaymentAnomalyFilters, keys: readonly string[], p: number, size: number) {
  const q = pagination(p, size);
  for (const [key, value] of Object.entries(filters)) {
    if (!keys.includes(key)) invalid();
    if (value === undefined || value === '') continue;
    if (typeof value !== 'string') invalid();
    if (key === 'channel') inputChannel(value);
    if (key === 'status' && !(keys.includes('orderNo') ? ['pending', 'credited', 'closed', 'refunded'] : ['open', 'resolved']).includes(value)) invalid();
    if (key === 'targetKind' && !['personal', 'team'].includes(value)) invalid();
    if (key === 'start' || key === 'end') { if (!Number.isFinite(Date.parse(value))) invalid(); }
    else inputID(value);
    q.set(key, value);
  }
  if (q.has('start') && q.has('end') && Date.parse(q.get('start')!) >= Date.parse(q.get('end')!)) invalid();
  return q;
}

export function createPaymentApi(transport = createHttpClient()): PaymentAPI {
  const read = transport.json;
  const userOrder = (no: string) => '/api/payments/orders/' + encodeURIComponent(inputID(no));
  const opsOrder = (no: string) => '/api/operations/payments/orders/' + encodeURIComponent(inputID(no));
  return {
    async options(signal) { return options(await read('/api/payments/options', { signal })); },
    async createOrder(input) {
      const command = {
        clientRequestId: inputID(input.clientRequestId),
        target: inputTarget(input.target),
        packageId: inputID(input.packageId),
        channel: inputChannel(input.channel),
        agreementVersion: inputID(input.agreementVersion)
      };
      const saved = order(await read('/api/payments/orders', jsonPost(command)));
      if (saved.clientRequestId !== command.clientRequestId || saved.target.kind !== command.target.kind || saved.target.id !== command.target.id || saved.packageId !== command.packageId || saved.channel !== command.channel || saved.agreementVersion !== command.agreementVersion) malformed();
      return saved;
    },
    async getOrder(no, signal) { return matchingOrder(await read(userOrder(no), { signal }), no); },
    async closeOrder(no) { return matchingOrder(await read(userOrder(no) + '/close', jsonPost({})), no); },
    async orders(p = 1, size = 20, signal) {
      return page(await read('/api/payments/orders?' + pagination(p, size), { signal }), order, p, size);
    },
    async simulate(no, result) {
      if (result !== 'paid' && result !== 'failed') invalid();
      return matchingOrder(await read('/api/payments/demo/orders/' + encodeURIComponent(inputID(no)) + '/simulate', jsonPost({ result })), no);
    },
    async operationsOrders(filters = {}, p = 1, size = 20, signal) {
      const query = filterQuery(filters, ['orderNo', 'channelTradeNo', 'buyerUserId', 'targetKind', 'targetId', 'channel', 'status', 'start', 'end'], p, size);
      return page(await read('/api/operations/payments/orders?' + query, { signal }), order, p, size);
    },
    async orderDetails(no, signal) { return details(await read(opsOrder(no), { signal }), no); },
    async queryOrder(no, why) {
      return matchingOrder(await read(opsOrder(no) + '/query', jsonPost({ reason: reason(why) })), no);
    },
    async refundQuote(no, credits = 0, signal) {
      inputInteger(credits);
      const out = quote(await read(opsOrder(no) + '/refund-quote?credits=' + credits, { signal }));
      if (credits > 0 && out.rechargeCredits !== credits) malformed();
      return out;
    },
    async createRefund(no, input) {
      const command = {
        refundId: inputID(input.refundId),
        credits: inputInteger(input.credits, true),
        reason: reason(input.reason)
      };
      const saved = refund(await read(opsOrder(no) + '/refunds', jsonPost(command)));
      if (saved.id !== command.refundId || saved.orderNo !== no || saved.kind !== 'operator' || saved.rechargeCredits !== command.credits || saved.reason !== command.reason) malformed();
      return saved;
    },
    async anomalies(filters = {}, p = 1, size = 20, signal) {
      const query = filterQuery(filters, ['status', 'kind', 'channel'], p, size);
      return page(await read('/api/operations/payments/anomalies?' + query, { signal }), anomaly, p, size);
    },
    async resolveAnomaly(id, note) {
      const saved = anomaly(await read('/api/operations/payments/anomalies/' + encodeURIComponent(inputID(id)) + '/resolve', jsonPost({ note: reason(note) })));
      if (saved.id !== id || saved.note !== note || saved.status !== 'resolved' || !saved.resolvedBy || !saved.resolvedAt) malformed();
      return saved;
    },
    async packages(signal) {
      const v = object(await read('/api/operations/payments/packages', { signal }));
      return unique(array(v.items, namedPaymentPackage), p => p.id);
    },
    async savePackage(input) {
      const command = {
        id: inputID(input.id),
        name: inputPackageName(input.name),
        originalAmountFen: inputInteger(input.originalAmountFen, true),
        revision: inputInteger(input.revision),
        amountFen: inputInteger(input.amountFen, true),
        rechargeCredits: inputInteger(input.rechargeCredits, true),
        giftCredits: inputInteger(input.giftCredits),
        enabled: input.enabled,
        sort: input.sort,
        testOnly: input.testOnly
      };
      if (command.originalAmountFen < command.amountFen || command.revision === Number.MAX_SAFE_INTEGER || typeof command.enabled !== 'boolean' || typeof command.testOnly !== 'boolean' || !Number.isSafeInteger(command.sort) || command.sort < -2147483648 || command.sort > 2147483647 || BigInt(command.rechargeCredits) + BigInt(command.giftCredits) > BigInt(Number.MAX_SAFE_INTEGER)) invalid();
      const saved = namedPaymentPackage(await read('/api/operations/payments/packages', jsonPost(command)));
      if (saved.revision !== command.revision + 1 || (['id', 'name', 'originalAmountFen', 'amountFen', 'rechargeCredits', 'giftCredits', 'enabled', 'sort', 'testOnly'] as const).some(key => saved[key] !== command[key])) malformed();
      return saved;
    },
    async settings(signal) { return settings(await read('/api/operations/payments/settings', { signal })); },
    async saveSettings(input) {
      const command = {
        revision: inputInteger(input.revision),
        dailyLimitFen: inputInteger(input.dailyLimitFen)
      };
      if (command.revision === Number.MAX_SAFE_INTEGER) invalid();
      const saved = settings(await read('/api/operations/payments/settings', jsonPost(command)));
      if (saved.revision !== command.revision + 1 || saved.dailyLimitFen !== command.dailyLimitFen) malformed();
      return saved;
    },
  };
}
export const paymentApi = createPaymentApi();
