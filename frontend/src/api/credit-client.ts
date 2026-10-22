import { responseFields } from './response-fields';
import { watchInvalidations } from './invalidation-stream';
import { jsonPost, type createHttpClient } from './client';
import { ApiError } from './http-error';
import { pageView, safeInteger, creditInput } from './response-values';
import type {
  Administration,
  CreditBalanceView,
  CreditFilters,
  CreditPageSize,
  CreditLedgerPage,
  CreditPolicyView,
  LedgerView,
} from './administration';
import type {
  CreditBalanceDTO,
  CreditLedgerDTO,
  CreditPolicyDTO,
  CreditReceiptDTO,
  DailyGiftDTO,
} from './administration-types';
import { translate } from '../contexts/translate';
const malformed = () =>
  new ApiError(200, 'PROGRAM_FAILED', translate('积分回执与模块合同不一致，未采用响应。'));
export function creditBalanceView(value: CreditBalanceDTO): CreditBalanceView {
  value = responseFields(value, ['balance', 'rechargeBalance', 'giftBalance', 'legacyBalance']);
  const mapped = {
    balance: safeInteger(value.balance),
    rechargeBalance: safeInteger(value.rechargeBalance),
    giftBalance: safeInteger(value.giftBalance),
    legacyBalance: safeInteger(value.legacyBalance),
  };
  if (
    BigInt(mapped.rechargeBalance) + BigInt(mapped.giftBalance) + BigInt(mapped.legacyBalance) !==
    BigInt(mapped.balance)
  )
    throw malformed();
  return mapped;
}
export function creditLedgerView(value: CreditReceiptDTO): LedgerView {
  value = responseFields(value, [
    'id',
    'operationId',
    'accountId',
    'kind',
    'amount',
    'balance',
    'category',
    'rechargeAmount',
    'giftAmount',
    'legacyAmount',
    'source',
    'subject',
    'reason',
    'originalDeductionId',
    'originalAdditionId',
    'createdAt',
  ]);
  if (
    [
      value.id,
      value.operationId,
      value.accountId,
      value.source,
      value.subject,
      value.reason,
      value.createdAt,
    ].some((field) => typeof field !== 'string' || !field) ||
    (value.originalDeductionId !== undefined &&
      (typeof value.originalDeductionId !== 'string' || !value.originalDeductionId)) ||
    (value.originalAdditionId !== undefined &&
      (typeof value.originalAdditionId !== 'string' || !value.originalAdditionId)) ||
    (value.kind === 'refund' && !value.originalDeductionId) ||
    (value.kind === 'reclaim' && (!value.originalAdditionId || !!value.originalDeductionId || !['recharge', 'gift'].includes(value.category))) ||
    (value.kind !== 'reclaim' && !!value.originalAdditionId) ||
    (value.kind === 'expire' && (value.category !== 'gift' || !!value.originalDeductionId)) ||
    !['add', 'deduct', 'refund', 'expire', 'reclaim'].includes(value.kind) ||
    !['recharge', 'gift', 'legacy', 'mixed'].includes(value.category) ||
    !value.source ||
    !value.subject ||
    !value.reason ||
    !Number.isFinite(Date.parse(value.createdAt))
  )
    throw malformed();
  const amount = safeInteger(value.amount),
    rechargeAmount = safeInteger(value.rechargeAmount),
    giftAmount = safeInteger(value.giftAmount),
    legacyAmount = safeInteger(value.legacyAmount);
  if (
    BigInt(amount) <= 0n ||
    BigInt(amount) !== BigInt(rechargeAmount) + BigInt(giftAmount) + BigInt(legacyAmount)
  )
    throw malformed();
  const present = [
    ['recharge', rechargeAmount],
    ['gift', giftAmount],
    ['legacy', legacyAmount],
  ].filter(([, part]) => BigInt(part) > 0n);
  if (value.category !== (present.length === 1 ? present[0][0] : 'mixed')) throw malformed();
  return {
    identity: value.id,
    at: value.createdAt,
    direction: value.kind,
    category: value.category,
    rechargeAmount,
    giftAmount,
    legacyAmount,
    amount,
    balance: safeInteger(value.balance),
    reason: value.reason,
    source: value.source,
    originalDebit: value.originalDeductionId || null,
    originalAddition: value.originalAdditionId || null,
    actorLabel: value.subject,
  };
}
function ledgerPage(value: CreditLedgerDTO): CreditLedgerPage {
  value = responseFields(value, ['items', 'total', 'page', 'pageSize', 'summary', 'analysis']);
  value.summary = responseFields(value.summary, [
    'acquired',
    'consumed',
    'refunded',
    'netConsumed',
    'settledConsumed',
    'pending',
  ]);
  if (!Array.isArray(value.analysis)) throw malformed();
  const summary = {
    acquired: safeInteger(value.summary.acquired),
    consumed: safeInteger(value.summary.consumed),
    refunded: safeInteger(value.summary.refunded),
    netConsumed: safeInteger(value.summary.netConsumed),
    settledConsumed: safeInteger(value.summary.settledConsumed),
    pending: safeInteger(value.summary.pending),
  };
  const seen = new Set<string>();
  const analysis = value.analysis.map((row) => {
    row = responseFields(row, ['source', 'amount']);
    if (
      !['language-generation', 'media-generation', 'operations-adjustment'].includes(row.source) ||
      seen.has(row.source)
    )
      throw malformed();
    seen.add(row.source);
    return { source: row.source, amount: safeInteger(row.amount) };
  });
  if (
    analysis.reduce((sum, row) => sum + BigInt(row.amount), 0n) !== BigInt(summary.settledConsumed)
  )
    throw malformed();
  return { ...pageView(value, creditLedgerView), summary, analysis };
}
function policyView(value: CreditPolicyDTO): CreditPolicyView {
  value = responseFields(value, ['revision', 'registrationGift', 'dailyGift', 'timezone']);
  safeInteger(value.revision);
  if (value.timezone !== 'Asia/Shanghai') throw malformed();
  return {
    revision: value.revision,
    registrationGift: safeInteger(value.registrationGift),
    dailyGift: safeInteger(value.dailyGift),
    timezone: value.timezone,
  };
}
function query(page = '1', filters?: CreditFilters, pageSize: CreditPageSize = '10') {
  if (!['10', '20', '50'].includes(pageSize)) throw malformed();
  if (!/^[1-9][0-9]*$/.test(page || '1') || !Number.isSafeInteger(Number(page || '1')))
    throw malformed();
  const params = new URLSearchParams({ page: page || '1', pageSize });
  if (filters) {
    if (
      !['all', 'acquired', 'consumed', 'expired', 'reclaimed'].includes(filters.direction) ||
      !['all', 'recharge', 'gift', 'legacy'].includes(filters.category)
    )
      throw malformed();
    for (const key of ['direction', 'category'] as const)
      if (filters[key] !== 'all') params.set(key, filters[key]);
    for (const key of ['start', 'end'] as const)
      if (filters[key]) {
        if (!Number.isFinite(Date.parse(filters[key]))) throw malformed();
        params.set(key, filters[key]);
      }
    if (filters.start && filters.end && Date.parse(filters.start) >= Date.parse(filters.end))
      throw new ApiError(400, 'CREDIT_QUERY_INVALID', translate('结束时间须晚于开始时间。'));
  }
  return params;
}
export function createCreditApi(
  transport: ReturnType<typeof createHttpClient>,
): Pick<
  Administration,
  | 'myCreditBalance'
  | 'watchCredits'
  | 'myLedger'
  | 'myRecentLedger'
  | 'userWallet'
  | 'adjust'
  | 'adjustment'
  | 'creditPolicy'
  | 'saveCreditPolicy'
  | 'dailyGift'
> {
  const read = transport.json;
  const userPath = (identity: string) =>
    '/api/operations/users/' + encodeURIComponent(identity) + '/credits';
  return {
    watchCredits(target, onChange, signal) {
      return watchInvalidations(
        transport,
        target === null ? '/api/credits/events' : userPath(target) + '/events',
        'credits',
        onChange,
        signal,
        'CREDIT_STREAM_CLOSED',
      );
    },
    async myCreditBalance() {
      return creditBalanceView(await read('/api/credits/balance'));
    },
    async myLedger(page, filters, pageSize) {
      return ledgerPage(await read('/api/credits/ledger?' + query(page, filters, pageSize)));
    },
    async myRecentLedger() {
      const value = responseFields(await read<{ items: CreditReceiptDTO[] }>('/api/credits/ledger/recent'), ['items']);
      if (!Array.isArray(value.items) || value.items.length > 5) throw malformed();
      return value.items.map(creditLedgerView);
    },
    async userWallet(identity, page, pageSize) {
      const base = userPath(identity);
      const [balance, ledger] = await Promise.all([
        read<CreditBalanceDTO>(base + '/balance'),
        read<CreditLedgerDTO>(base + '/ledger?' + query(page, undefined, pageSize)),
      ]);
      return { ...creditBalanceView(balance), ledger: ledgerPage(ledger) };
    },
    async adjust(input) {
      const amount = creditInput(input.amount);
      if (
        (input.direction === 'add' && !['recharge', 'gift'].includes(input.category || '')) ||
        (input.direction === 'deduct' && input.category !== undefined)
      )
        throw new ApiError(
          400,
          'CREDIT_INVALID_CATEGORY',
          translate('新增积分须选择充值或赠送类型；扣减按余额顺序分配。'),
        );
      const receipt = await read<CreditReceiptDTO>(
        userPath(input.account),
        jsonPost({
          kind: input.direction,
          amount,
          operationId: input.command,
          reason: input.reason,
          ...(input.direction === 'add' ? { category: input.category } : {}),
        }),
      );
      if (
        receipt.operationId !== input.command ||
        receipt.accountId !== input.account ||
        receipt.kind !== input.direction ||
        safeInteger(receipt.amount) !== input.amount ||
        receipt.reason !== input.reason ||
        (input.direction === 'add' && receipt.category !== input.category)
      )
        throw malformed();
      return { state: 'committed', entry: creditLedgerView(receipt) };
    },
    async adjustment(command) {
      try {
        const receipt = await read<CreditReceiptDTO>(
          '/api/operations/credit-operations/' + encodeURIComponent(command),
        );
        if (receipt.operationId !== command) throw malformed();
        return { state: 'committed', entry: creditLedgerView(receipt) };
      } catch (error) {
        if (error instanceof ApiError && error.status === 404 && error.code === 'NOT_FOUND')
          return {
            state: 'absent',
            message: translate('服务端确认未找到该操作；可使用同一编号重试。'),
          };
        throw error;
      }
    },
    async creditPolicy() {
      return policyView(await read('/api/operations/credit-policy'));
    },
    async saveCreditPolicy(input) {
      for (const amount of [input.revision, input.registrationGift, input.dailyGift])
        if (!Number.isSafeInteger(amount) || amount < 0)
          throw new ApiError(
            400,
            'CREDIT_POLICY_INVALID',
            translate('赠送积分须为安全范围内的非负整数。'),
          );
      const saved = policyView(
        await read(
          '/api/operations/credit-policy',
          jsonPost({
            revision: input.revision,
            registrationGift: input.registrationGift,
            dailyGift: input.dailyGift,
          }),
        ),
      );
      if (
        saved.revision !== input.revision + 1 ||
        saved.registrationGift !== String(input.registrationGift) ||
        saved.dailyGift !== String(input.dailyGift)
      )
        throw malformed();
      return saved;
    },
    async dailyGift() {
      let value = await read<DailyGiftDTO>('/api/credits/daily-gift', jsonPost({}));
      value = responseFields(value, ['date', 'amount', 'claimed', 'alreadyClaimed']);
      const amount = safeInteger(value.amount);
      if (
        !/^\d{4}-\d{2}-\d{2}$/.test(value.date) ||
        value.claimed !== (BigInt(amount) > 0n) ||
        typeof value.alreadyClaimed !== 'boolean' ||
        (value.alreadyClaimed && !value.claimed)
      )
        throw malformed();
      return {
        date: value.date,
        amount,
        claimed: value.claimed,
        alreadyClaimed: value.alreadyClaimed,
      };
    },
  };
}
