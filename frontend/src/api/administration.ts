import { ApiError } from './http-error';
import { http, jsonPost, type createHttpClient } from './client';
import { createUsageApi } from './model-usage-client';
import { createSmtpApi } from './smtp-client';
import { createCreditApi } from './credit-client';
export { creditLedgerView } from './credit-client';
import { pageView } from './response-values';
export { pageView, safeInteger, creditInput } from './response-values';
import { translate } from '../contexts/translate';
import type {
  UserDTO,
  ProfileDTO,
  PageDTO,
  AuthorizationCodeDTO,
  SmtpSettingsDTO,
  SmtpUpdateDTO,
  CreditCategory,
  CreditPolicyDTO,
} from './administration-types';

// Presentation values map the backend-owned wire DTOs below; no Actor or Cookie is stored here.
export type SessionView = {
  identity: string;
  username: string;
  email: string;
  avatarUrl?: string | null;
  operationsAllowed: boolean;
  roleLabel: string;
};
export type AuthForm = {
  username: string;
  email: string;
  password: string;
  emailCode: string;
  registrationGrant: string;
};
export type AuthOptionsView = {
  registrationGrantRequired: boolean;
  passwordLogin: boolean;
  emailLogin: boolean;
};
export type PageView<T> = {
  items: T[];
  previous: string | null;
  next: string | null;
  caption: string;
};
export type UserView = {
  identity: string;
  username: string;
  email: string;
  roleLabel: string;
  enabled: boolean;
  createdAt: string;
};
export type GrantView = {
  identity: string;
  expiresAt: string;
  stateLabel: string;
  canDisable: boolean;
  usedBy: string | null;
  usedAt: string | null;
};
export type LedgerView = {
  category?: CreditCategory;
  rechargeAmount?: string;
  giftAmount?: string;
  legacyAmount?: string;
  identity: string;
  at: string;
  direction: 'add' | 'deduct' | 'refund' | 'expire' | 'reclaim';
  amount: string;
  balance: string;
  reason: string;
  source: string;
  originalDebit: string | null;
  originalAddition: string | null;
  actorLabel: string;
};
export type CreditBalanceView = {
  balance: string;
  rechargeBalance: string;
  giftBalance: string;
  legacyBalance: string;
};
export type CreditPageSize = '10' | '20' | '50';
export type CreditFilters = {
  direction: 'all' | 'acquired' | 'consumed' | 'expired' | 'reclaimed';
  category: 'all' | 'recharge' | 'gift' | 'legacy';
  start: string;
  end: string;
};
export type CreditLedgerPage = PageView<LedgerView> & {
  summary: {
    acquired: string;
    consumed: string;
    refunded: string;
    netConsumed: string;
    settledConsumed: string;
    pending: string;
  };
  analysis: { source: string; amount: string }[];
};
export type CreditPolicyView = {
  revision: number;
  registrationGift: string;
  dailyGift: string;
  timezone: 'Asia/Shanghai';
};
export type DailyGiftView = {
  date: string;
  amount: string;
  claimed: boolean;
  alreadyClaimed: boolean;
};
export type WalletView = CreditBalanceView & { ledger: CreditLedgerPage };
export type Adjustment = {
  command: string;
  account: string;
  direction: 'add' | 'deduct';
  amount: string;
  reason: string;
  category?: 'recharge' | 'gift';
};
export type AdjustmentResult =
  | { state: 'committed'; entry: LedgerView }
  | { state: 'pending' | 'absent'; message: string };
export type ChargeView = {
  quantity: string;
  unit: 'characters' | 'images' | 'seconds';
  unitPrice: string;
  original: string;
  due: string;
  free: boolean;
  freeStart: string | null;
  freeEnd: string | null;
};
export type UsageFilters = {
  spaceId?: string;
  start: string;
  end: string;
  search: string;
  searchBy?: 'userId' | 'taskId' | 'requestId';
  model: string;
  connection: string;
  business: string;
  result: string;
};
export type UsageView = 'tasks' | 'calls';
export type UsagePageSize = '10' | '20' | '50';
export type UsageRow = {
  spaceId?: string;
  spaceName?: string;
  spaceKind?: 'personal' | 'team';
  identity: string;
  at: string;
  userLabel: string;
  userEmail: string | null;
  userIdentity: string;
  operation: string;
  modelLabel: string;
  connectionLabel: string;
  executionLabel: string;
  settlementLabel: string;
  charge: ChargeView | null;
  deducted: string | null;
  reserved: string | null;
  refunded: string | null;
  costCny: string | null;
  costPending?: boolean;
  taskIdentity: string;
  inputTokens: string | null;
  outputTokens: string | null;
  durationLabel: string | null;
  mediaUsageLabel?: string | null;
  requestIdentity: string | null;
};
export type UsageDetail = {
  charge: ChargeView | null;
  callCaption?: string;
  safeError: string | null;
  calls: UsageRow[];
};
export type UsagePage = PageView<UsageRow> & {
  summary: {
    scopeLabel: string;
    count: string;
    deducted: string;
    refunded: string;
    knownCostCny: string;
    hasUnknownCost: boolean;
    readAt: string;
  };
};
export type FilterOption = { value: string; label: string };
export type UsageOptions = {
  models: FilterOption[];
  connections: FilterOption[];
  businesses: FilterOption[];
  results: FilterOption[];
};
export interface Administration {
  current(): Promise<SessionView | null>;
  authOptions(): Promise<AuthOptionsView>;
  login(mode: 'password' | 'email', form: AuthForm, operations: boolean): Promise<void>;
  register(form: AuthForm): Promise<void>;
  sendCode(email: string, purpose: 'register' | 'login' | 'reset'): Promise<void>;
  resetPassword(form: AuthForm): Promise<void>;
  logout(): Promise<void>;
  saveProfile(username: string): Promise<SessionView>;
  uploadAvatar(file: File): Promise<SessionView>;
  removeAvatar(): Promise<SessionView>;
  changePassword(currentPassword: string, password: string): Promise<void>;
  smtp(): Promise<SmtpSettingsDTO>;
  saveSmtp(input: SmtpUpdateDTO): Promise<SmtpSettingsDTO>;
  // Exact stable codes must be mapped by the production adapter, not inferred.
  sessionMessage(code: string): string | null;
  users(search: string, page?: string): Promise<PageView<UserView>>;
  setUserEnabled(identity: string, enabled: boolean): Promise<void>;
  registration(): Promise<{ required: boolean }>;
  setRegistration(required: boolean): Promise<void>;
  grants(page?: string): Promise<PageView<GrantView>>;
  createGrant(expiresAt: string): Promise<{ plaintext: string }>;
  disableGrant(identity: string): Promise<void>;
  myCreditBalance(): Promise<CreditBalanceView>;
  watchCredits(target: string | null, onChange: () => void, signal: AbortSignal): Promise<void>;
  myLedger(page?: string, filters?: CreditFilters, pageSize?: CreditPageSize): Promise<CreditLedgerPage>;
  myRecentLedger(): Promise<LedgerView[]>;
  creditPolicy(): Promise<CreditPolicyView>;
  saveCreditPolicy(
    input: Pick<CreditPolicyDTO, 'revision' | 'registrationGift' | 'dailyGift'>,
  ): Promise<CreditPolicyView>;
  dailyGift(): Promise<DailyGiftView>;
  userWallet(identity: string, page?: string, pageSize?: CreditPageSize): Promise<WalletView>;
  adjust(input: Adjustment): Promise<AdjustmentResult>;
  adjustment(command: string): Promise<AdjustmentResult>;
  usageOptions(): Promise<UsageOptions>;
  usage(
    view: UsageView,
    filters: UsageFilters,
    page?: string,
    pageSize?: UsagePageSize,
  ): Promise<UsagePage>;
  usageDetail(view: UsageView, identity: string): Promise<UsageDetail>;
}
const malformed = () =>
  new ApiError(
    200,
    'PROGRAM_FAILED',
    translate('服务端回执与模块合同不一致，未采用响应。'),
  );
function userView(v: UserDTO): UserView {
  if (
    !v?.id ||
    !v.email ||
    typeof v.username !== 'string' ||
    !v.username ||
    !['user', 'operator'].includes(v.role) ||
    !['enabled', 'disabled'].includes(v.status)
  )
    throw malformed();
  return {
    identity: v.id,
    username: v.username,
    email: v.email,
    roleLabel: translate(v.role === 'operator' ? '运营人员' : '普通用户'),
    enabled: v.status === 'enabled',
    createdAt: v.createdAt,
  };
}
function paging(page = '') {
  if (page && !/^[1-9][0-9]*$/.test(page)) throw malformed();
  const p = page ? Number(page) : 1;
  if (!Number.isSafeInteger(p)) throw malformed();
  return new URLSearchParams({ page: String(p), pageSize: '20' });
}
function sessionView(v: ProfileDTO): SessionView {
  if (!v) throw malformed();
  userView(v.user);
  if (
    v.avatarUrl !== null &&
    (typeof v.avatarUrl !== 'string' || !/^\/api\/auth\/avatar\?v=[0-9a-f]{64}$/.test(v.avatarUrl))
  )
    throw malformed();
  return {
    identity: v.user.id,
    username: v.user.username,
    email: v.user.email,
    avatarUrl: v.avatarUrl ?? null,
    operationsAllowed: v.user.role === 'operator',
    roleLabel: translate(v.user.role === 'operator' ? '运营人员' : '普通用户'),
  };
}
export function createAdministrationApi(
  transport: ReturnType<typeof createHttpClient> = http,
): Administration {
  const read = transport.json;
  const publicPost = (body: unknown) => ({ ...jsonPost(body), authentication: 'public' as const });
  const userPath = (id: string) => '/api/operations/users/' + encodeURIComponent(id);
  return {
    ...createUsageApi(transport),
    ...createSmtpApi(transport),
    ...createCreditApi(transport),
    async current() {
      try {
        const v = await read<ProfileDTO>('/api/auth/me', { authentication: 'public' });
        return sessionView(v);
      } catch (e) {
        if (e instanceof ApiError && e.code === 'AUTH_REQUIRED') return null;
        throw e;
      }
    },
    async authOptions() {
      const v = await read<{
        registrationRequiresCode: boolean;
        passwordLogin: boolean;
        codeLogin: boolean;
      }>('/api/auth/options', { authentication: 'public' });
      if (
        typeof v.registrationRequiresCode !== 'boolean' ||
        typeof v.passwordLogin !== 'boolean' ||
        typeof v.codeLogin !== 'boolean'
      )
        throw malformed();
      return {
        registrationGrantRequired: v.registrationRequiresCode,
        passwordLogin: v.passwordLogin,
        emailLogin: v.codeLogin,
      };
    },
    async login(mode, form, operations) {
      const v = await read<{ user: UserDTO }>(
        '/api/auth/login',
        publicPost({
          email: form.email,
          ...(mode === 'password' ? { password: form.password } : { code: form.emailCode }),
          audience: operations ? 'operations' : 'workspace',
        }),
      );
      userView(v.user);
      if (operations && v.user.role !== 'operator')
        throw new ApiError(403, 'ROLE_FORBIDDEN', translate('当前账号没有运营权限。'));
    },
    async register(form) {
      const v = await read<UserDTO>(
        '/api/auth/register',
        publicPost({
          email: form.email,
          username: form.username,
          password: form.password,
          code: form.emailCode,
          ...(form.registrationGrant ? { authorizationCode: form.registrationGrant } : {}),
        }),
      );
      userView(v);
    },
    async sendCode(email, purpose) {
      const v = await read<{ submitted: boolean }>(
        '/api/auth/email-code',
        publicPost({ email, purpose }),
      );
      if (v.submitted !== true) throw malformed();
    },
    async resetPassword(form) {
      const v = await read<{ updated: boolean }>(
        '/api/auth/reset-password',
        publicPost({ email: form.email, code: form.emailCode, password: form.password }),
      );
      if (v.updated !== true) throw malformed();
    },
    async logout() {
      const v = await read<{ loggedOut: boolean }>('/api/auth/logout', jsonPost({}));
      if (v.loggedOut !== true) throw malformed();
    },
    async saveProfile(username) {
      return sessionView(await read('/api/auth/profile', jsonPost({ username })));
    },
    async uploadAvatar(file) {
      const body = new FormData();
      body.append('file', file);
      return sessionView(await read('/api/auth/avatar', { method: 'POST', body }));
    },
    async removeAvatar() {
      return sessionView(await read('/api/auth/avatar', { method: 'DELETE' }));
    },
    async changePassword(currentPassword, password) {
      const v = await read<{ updated: boolean }>(
        '/api/auth/password',
        jsonPost({ currentPassword, password }),
      );
      if (v.updated !== true) throw malformed();
    },
    sessionMessage(code) {
      const messages: Record<string, string> = {
        SESSION_REPLACED: translate('账号已在其他位置登录，请重新登录。'),
        SESSION_REVOKED: translate('登录已撤销，请重新登录。'),
        SESSION_EXPIRED: translate('会话已过期，请重新登录。'),
        ACCOUNT_DISABLED: translate('账号已禁用。'),
        AUTH_REQUIRED: translate('请先登录。'),
        INVALID_CREDENTIALS: translate('邮箱、密码或验证码不正确。'),
      };
      return messages[code] || null;
    },
    async users(search, page) {
      const q = paging(page);
      if (search) q.set('q', search);
      return pageView(await read<PageDTO<UserDTO>>('/api/operations/users?' + q), userView);
    },
    async setUserEnabled(identity, enabled) {
      userView(await read<UserDTO>(userPath(identity) + '/status', jsonPost({ enabled })));
    },
    async registration() {
      const v = await read<{ registrationRequiresCode: boolean }>('/api/operations/registration');
      if (typeof v.registrationRequiresCode !== 'boolean') throw malformed();
      return { required: v.registrationRequiresCode };
    },
    async setRegistration(required) {
      const v = await read<{ registrationRequiresCode: boolean }>(
        '/api/operations/registration',
        jsonPost({ registrationRequiresCode: required }),
      );
      if (v.registrationRequiresCode !== required) throw malformed();
    },
    async grants(page) {
      return pageView(
        await read<PageDTO<AuthorizationCodeDTO>>(
          '/api/operations/authorization-codes?' + paging(page),
        ),
        (v) => {
          if (
            !v?.id ||
            !Number.isFinite(Date.parse(v.createdAt)) ||
            !Number.isFinite(Date.parse(v.expiresAt)) ||
            typeof v.disabled !== 'boolean' ||
            (v.usedBy !== null && typeof v.usedBy !== 'string') ||
            (v.usedAt !== null && !Number.isFinite(Date.parse(v.usedAt)))
          )
            throw malformed();
          return {
            identity: v.id,
            expiresAt: v.expiresAt,
            stateLabel: translate(
              v.usedAt
                ? '已使用'
                : v.disabled
                  ? '已停用'
                  : Date.parse(v.expiresAt) <= Date.now()
                    ? '已过期'
                    : '未使用',
            ),
            canDisable: !v.disabled && !v.usedAt,
            usedBy: v.usedBy,
            usedAt: v.usedAt,
          };
        },
      );
    },
    async createGrant(expiresAt) {
      const v = await read<{ code: string }>(
        '/api/operations/authorization-codes',
        jsonPost({ expiresAt }),
      );
      if (typeof v.code !== 'string' || !v.code) throw malformed();
      return { plaintext: v.code };
    },
    async disableGrant(identity) {
      const v = await read<{ disabled: boolean }>(
        '/api/operations/authorization-codes/' + encodeURIComponent(identity) + '/disable',
        jsonPost({}),
      );
      if (v.disabled !== true) throw malformed();
    },
  };
}
export const administration = createAdministrationApi();
