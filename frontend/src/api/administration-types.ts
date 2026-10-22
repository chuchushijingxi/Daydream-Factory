// Wire DTOs from accounts/api.md and credits/api.md (backend owned).
export type UserDTO = {
  id: string;
  email: string;
  username: string;
  role: 'user' | 'operator';
  status: 'enabled' | 'disabled';
  createdAt: string;
};
export type ProfileDTO = { user: UserDTO; avatarUrl: string | null };
// operations/api.md owns the dynamic SMTP contract. Secrets are write-only.
export type SmtpSettingsDTO = {
  revision: number;
  enabled: boolean;
  address: string;
  username: string;
  from: string;
  tls: 'starttls' | 'tls';
  passwordConfigured: boolean;
};
export type SmtpUpdateDTO = Omit<SmtpSettingsDTO, 'passwordConfigured'> & {
  password?: string;
  clearPassword?: boolean;
};
export type PageDTO<T> = { items: T[]; total: number; page: number; pageSize: number };
export type AuthorizationCodeDTO = {
  id: string;
  createdAt: string;
  expiresAt: string;
  disabled: boolean;
  usedBy: string | null;
  usedAt: string | null;
};
export type CreditCategory = 'recharge' | 'gift' | 'legacy' | 'mixed';
export type CreditBalanceDTO = {
  balance: number;
  rechargeBalance: number;
  giftBalance: number;
  legacyBalance: number;
};
export type CreditReceiptDTO = {
  id: string;
  operationId: string;
  accountId: string;
  kind: 'add' | 'deduct' | 'refund' | 'expire' | 'reclaim';
  amount: number;
  balance: number;
  category: CreditCategory;
  rechargeAmount: number;
  giftAmount: number;
  legacyAmount: number;
  source: string;
  subject: string;
  reason: string;
  originalDeductionId?: string;
  originalAdditionId?: string;
  createdAt: string;
};
export type CreditSummaryDTO = {
  acquired: number;
  consumed: number;
  refunded: number;
  netConsumed: number;
  settledConsumed: number;
  pending: number;
};
export type CreditLedgerDTO = PageDTO<CreditReceiptDTO> & {
  summary: CreditSummaryDTO;
  analysis: {
    source: 'language-generation' | 'media-generation' | 'operations-adjustment';
    amount: number;
  }[];
};
export type CreditPolicyDTO = {
  revision: number;
  registrationGift: number;
  dailyGift: number;
  timezone: 'Asia/Shanghai';
};
export type DailyGiftDTO = {
  date: string;
  amount: number;
  claimed: boolean;
  alreadyClaimed: boolean;
};
// Source: modelusage.Record/Result and the single operations query handler.
export type CreditPriceDTO = {
  resolutionPrices?: Record<string, number>;
  unitPrice: number;
  freeEnabled: boolean;
  freeStart: string | null;
  freeEnd: string | null;
};
export type ChargeQuoteDTO = {
  mediaOptions?: import('./types').MediaOptions;
  basis: string;
  modelId: string;
  connectionId: string;
  scope: string;
  sourceVersion: number;
  inputRevision: number;
  quantity: number;
  unit: 'characters' | 'image' | 'second';
  priceVersion: number;
  rule: CreditPriceDTO;
  originalAmount: number;
  amount: number;
  free: boolean;
};
export type UsageBasisDTO = Omit<ChargeQuoteDTO, 'basis'> & {
  id: string;
  scopeDigest: string;
  acceptedAt: string;
};
export type UsageRecordDTO = {
  spaceId?: string;
  spaceName?: string;
  spaceKind?: 'personal' | 'team';
  id: string;
  taskId: string;
  userId: string;
  username: string | null;
  email: string | null;
  operation: string;
  status: string;
  modelId: string;
  connectionId: string | null;
  requestId: string | null;
  createdAt: string;
  durationMs: number | null;
  inputTokens: number | null;
  outputTokens: number | null;
  cacheHitTokens: number | null;
  mediaUsage: { imageCount: number; videoSeconds: number | null } | null;
  knownCostMicroCny: number;
  unknownCost: boolean;
  callCount: number;
  basis: UsageBasisDTO | null;
  prepaid: number;
  refunded: number;
  charged: number;
  settlement: string;
  deductionId: string | null;
  refundId: string | null;
  errorCode: string | null;
};
export type UsageResultDTO = PageDTO<UsageRecordDTO> & {
  stats: {
    count: number;
    callCount: number;
    charged: number;
    refunded: number;
    knownCostMicroCny: number;
    unknownCost: boolean;
  };
  readAt: string;
};
