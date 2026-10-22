import type {
  Administration,
  SessionView,
  UsageRow,
  UsagePage,
  UsageDetail,
  Adjustment,
  LedgerView,
  CreditLedgerPage,
  CreditBalanceView,
  CreditPolicyView,
} from '../src/api/administration';
import { administration } from '../src/api/administration';
import type { SmtpSettingsDTO } from '../src/api/administration-types';
import { ApiError } from '../src/api/http-error';
const at = '2026-10-16T06:32:18Z';
export const creditStats = {
  summary: {
    acquired: '0',
    consumed: '0',
    refunded: '0',
    netConsumed: '0',
    settledConsumed: '0',
    pending: '0',
  },
  analysis: [],
} satisfies Pick<CreditLedgerPage, 'summary' | 'analysis'>;
export function createAdministrationFixture() {
  let user: SessionView | null = {
    identity: 'fixture_operator',
    username: '虚构运营',
    email: 'operator@example.invalid',
    operationsAllowed: true,
    roleLabel: '运营人员',
  };
  let balance = '9007199254740990',
    grantRequired = true,
    adjustCount = 0,
    uncertain = false;
  let policy: CreditPolicyView = {
    revision: 0,
    registrationGift: '0',
    dailyGift: '0',
    timezone: 'Asia/Shanghai',
  };
  let claimedAmount = '0';
  let wallet: CreditBalanceView = {
    balance,
    rechargeBalance: (BigInt(balance) - 60n).toString(),
    giftBalance: '60',
    legacyBalance: '0',
  };
  let smtp: SmtpSettingsDTO = {
    revision: 0,
    enabled: false,
    address: '',
    username: '',
    from: '',
    tls: 'starttls',
    passwordConfigured: false,
  };
  const requests: { method: string; input?: unknown }[] = [],
    ledger: LedgerView[] = [],
    commands = new Map<string, LedgerView>();
  const rows: UsageRow[] = [
    {
      identity: 'fixture_task_01',
      at,
      userLabel: '虚构用户 A',
      userIdentity: 'fixture_a',
      userEmail: 'fiction-a@example.invalid',
      operation: '整剧解析',
      modelLabel: '虚构语言模型 A · 完整长名称用于验证可达性',
      connectionLabel: '官方连接',
      executionLabel: '成功',
      settlementLabel: '已结算',
      charge: {
        quantity: '18450',
        unit: 'characters',
        unitPrice: '2',
        original: '369',
        due: '369',
        free: false,
        freeStart: null,
        freeEnd: null,
      },
      deducted: '369',
      reserved: null,
      refunded: null,
      costCny: '0.820000',
      taskIdentity: 'fixture_task_01',
      inputTokens: '36800',
      outputTokens: '5200',
      durationLabel: '12 秒',
      requestIdentity: 'fixture_req_01',
    },
    {
      identity: 'fixture_task_02',
      at,
      userLabel: '虚构用户 B',
      userIdentity: 'fixture_b',
      userEmail: 'fiction-b@example.invalid',
      operation: '单集拆解',
      modelLabel: '虚构语言模型 A',
      connectionLabel: '官方连接',
      executionLabel: '成功',
      settlementLabel: '限免',
      charge: {
        quantity: '3200',
        unit: 'characters',
        unitPrice: '2',
        original: '64',
        due: '0',
        free: true,
        freeStart: '2026-10-16T00:00:00Z',
        freeEnd: '2026-10-17T00:00:00Z',
      },
      deducted: '0',
      reserved: null,
      refunded: null,
      costCny: '0.16',
      taskIdentity: 'fixture_task_02',
      inputTokens: '3200',
      outputTokens: '400',
      durationLabel: null,
      requestIdentity: null,
    },
    {
      identity: 'fixture_task_03',
      at,
      userLabel: '虚构用户 C',
      userIdentity: 'fixture_c',
      userEmail: 'fiction-c@example.invalid',
      operation: '整剧解析',
      modelLabel: '虚构语言模型 B',
      connectionLabel: '中转连接',
      executionLabel: '失败',
      settlementLabel: '已返还',
      charge: {
        quantity: '8000',
        unit: 'characters',
        unitPrice: '2',
        original: '160',
        due: '160',
        free: false,
        freeStart: null,
        freeEnd: null,
      },
      deducted: '0',
      reserved: null,
      refunded: '160',
      costCny: '0.340000123',
      taskIdentity: 'fixture_task_03',
      inputTokens: '12000',
      outputTokens: '500',
      durationLabel: '18 秒',
      requestIdentity: 'fixture_req_03',
    },
    {
      identity: 'fixture_task_04',
      at,
      userLabel: '虚构用户 D',
      userIdentity: 'fixture_d',
      userEmail: 'fiction-d@example.invalid',
      operation: '单集拆解',
      modelLabel: '虚构语言模型 A',
      connectionLabel: '官方连接',
      executionLabel: '执行中',
      settlementLabel: '未结算',
      charge: {
        quantity: '2500',
        unit: 'characters',
        unitPrice: '2',
        original: '50',
        due: '50',
        free: false,
        freeStart: null,
        freeEnd: null,
      },
      deducted: null,
      reserved: '50',
      refunded: null,
      costCny: null,
      taskIdentity: 'fixture_task_04',
      inputTokens: null,
      outputTokens: null,
      durationLabel: null,
      requestIdentity: null,
    },
  ];
  const detail = (row: UsageRow): UsageDetail => ({
    charge: row.charge,
    safeError: null,
    calls: [
      { ...row, identity: 'fixture_call_01' },
      { ...row, identity: 'fixture_call_02' },
    ],
  });
  const page = <T>(items: T[], key = '') => ({
    items,
    previous: key ? '' : null,
    next: key ? null : 'next',
    caption: key ? '第 2 页 · 虚构夹具' : '第 1 页 · 虚构夹具',
  });
  const record = (method: string, input?: unknown) => requests.push({ method, input });
  const service: Administration = {
    ...administration,
    async watchCredits(_target, _changed, signal) {
      if (signal.aborted) return;
      await new Promise<void>((resolve) =>
        signal.addEventListener('abort', () => resolve(), { once: true }),
      );
    },
    async current() {
      record('current');
      return user;
    },
    async authOptions() {
      return { registrationGrantRequired: grantRequired, passwordLogin: true, emailLogin: true };
    },
    async login(_mode, form, operations) {
      record('login', { email: form.email, operations });
      user = {
        identity: form.email,
        username: '虚构登录账号',
        email: form.email,
        operationsAllowed: operations,
        roleLabel: operations ? '运营人员' : '普通用户',
      };
    },
    async register(form) {
      record('register', { username: form.username, email: form.email });
    },
    async sendCode() {
      record('sendCode');
      return;
    },
    async resetPassword() {
      record('reset');
      user = null;
    },
    async logout() {
      record('logout');
      user = null;
    },
    async saveProfile(username) {
      record('saveProfile', username);
      user = { ...user!, username: username.trim() };
      return user;
    },
    async uploadAvatar(file) {
      record('uploadAvatar', { type: file.type, size: file.size });
      user = { ...user!, avatarUrl: '/api/auth/avatar?v=' + 'a'.repeat(64) };
      return user;
    },
    async removeAvatar() {
      record('removeAvatar');
      user = { ...user!, avatarUrl: null };
      return user;
    },
    async changePassword() {
      record('changePassword');
      user = null;
    },
    async smtp() {
      record('smtp');
      return { ...smtp };
    },
    async saveSmtp(input) {
      record('saveSmtp', input);
      if (input.revision !== smtp.revision)
        throw new ApiError(409, 'MAIL_CONFIGURATION_CONFLICT', '配置已被更新');
      const { enabled, address, username, from, tls } = input;
      smtp = {
        revision: smtp.revision + 1,
        enabled,
        address,
        username,
        from,
        tls,
        passwordConfigured: input.clearPassword
          ? false
          : !!input.password || smtp.passwordConfigured,
      };
      return { ...smtp };
    },
    sessionMessage(code) {
      return code === 'FIXTURE_REPLACED' ? '账号已在其他位置登录。' : null;
    },
    async users(search, key) {
      record('users', { search, key });
      return page(
        [
          {
            identity: 'fixture_a',
            username: '虚构用户 A',
            email: 'fiction@example.invalid',
            roleLabel: '普通用户',
            enabled: true,
            createdAt: at,
            lastLoginAt: at,
          },
        ],
        key,
      );
    },
    async setUserEnabled(identity, enabled) {
      record('setUserEnabled', { identity, enabled });
    },
    async registration() {
      record('registration');
      return { required: grantRequired, version: 'fixture_v1' };
    },
    async setRegistration(required) {
      record('setRegistration', required);
      grantRequired = required;
    },
    async grants(key) {
      return page(
        [
          {
            identity: 'fixture_grant_01',
            expiresAt: '2026-11-01T00:00:00Z',
            stateLabel: '未使用',
            canDisable: true,
            usedBy: null,
            usedAt: null,
          },
        ],
        key,
      );
    },
    async createGrant() {
      record('createGrant');
      return { plaintext: 'TEST-ONLY-NOT-A-REAL-GRANT' };
    },
    async disableGrant() {
      record('disableGrant');
    },
    async myCreditBalance() {
      record('myCreditBalance');
      return { ...wallet };
    },
    async myRecentLedger() {
      record('myRecentLedger');
      return ledger.slice(0, 5);
    },
    async myLedger(key, filters) {
      record('myLedger', key);
      record('creditFilters', filters);
      const items = ledger.filter(
        (row) =>
          !filters ||
          filters.direction === 'all' ||
          (filters.direction === 'acquired' && row.direction === 'add') ||
          (filters.direction === 'consumed' && ['deduct','refund'].includes(row.direction)) ||
          (filters.direction === 'expired' && row.direction === 'expire') ||
          (filters.direction === 'reclaimed' && row.direction === 'reclaim'),
      );
      return { ...page(items, key), ...creditStats };
    },
    async userWallet(identity, key) {
      record('userWallet', { identity, key });
      return { ...wallet, ledger: { ...page(ledger, key), ...creditStats } };
    },
    async creditPolicy() {
      record('creditPolicy');
      return { ...policy };
    },
    async saveCreditPolicy(input) {
      record('saveCreditPolicy', input);
      if (input.revision !== policy.revision)
        throw new ApiError(409, 'CREDIT_POLICY_CONFLICT', '设置冲突');
      policy = {
        revision: policy.revision + 1,
        registrationGift: String(input.registrationGift),
        dailyGift: String(input.dailyGift),
        timezone: 'Asia/Shanghai',
      };
      return { ...policy };
    },
    async dailyGift() {
      record('dailyGift');
      const alreadyClaimed = BigInt(claimedAmount) > 0n;
      if (!alreadyClaimed) claimedAmount = policy.dailyGift;
      return {
        date: '2026-10-17',
        amount: claimedAmount,
        claimed: BigInt(claimedAmount) > 0n,
        alreadyClaimed,
      };
    },
    async adjust(input: Adjustment) {
      record('adjust', input);
      adjustCount++;
      const entry: LedgerView = {
        category: input.category || 'gift',
        rechargeAmount: input.category === 'recharge' ? input.amount : '0',
        giftAmount: input.category === 'recharge' ? '0' : input.amount,
        legacyAmount: '0',
        identity: 'fixture_entry_' + adjustCount,
        at,
        direction: input.direction,
        amount: input.amount,
        balance: (
          BigInt(balance) +
          (input.direction === 'add' ? 1n : -1n) * BigInt(input.amount)
        ).toString(),
        reason: input.reason,
        source: '运营调整',
        originalDebit: null,
        actorLabel: '虚构运营',
      };
      commands.set(input.command, entry);
      ledger.unshift(entry);
      balance = entry.balance;
      wallet = {
        balance,
        rechargeBalance:
          input.category === 'recharge'
            ? (
                BigInt(wallet.rechargeBalance) +
                (input.direction === 'add' ? 1n : -1n) * BigInt(input.amount)
              ).toString()
            : wallet.rechargeBalance,
        giftBalance:
          input.category === 'recharge'
            ? wallet.giftBalance
            : (
                BigInt(wallet.giftBalance) +
                (input.direction === 'add' ? 1n : -1n) * BigInt(input.amount)
              ).toString(),
        legacyBalance: wallet.legacyBalance,
      };
      if (uncertain) throw new Error('夹具：已提交但响应丢失');
      return { state: 'committed', entry };
    },
    async adjustment(command) {
      record('adjustment', command);
      const entry = commands.get(command);
      return entry
        ? { state: 'committed', entry }
        : { state: 'absent', message: '未找到已提交回执，未重复发送。' };
    },
    async usageOptions() {
      return {
        models: [{ value: 'model_a', label: '虚构语言模型 A' }],
        connections: [{ value: 'official', label: '官方连接' }],
        businesses: [{ value: 'parse', label: '整剧解析' }],
        results: [{ value: 'success', label: '成功' }],
      };
    },
    async usage(view, filters, key, pageSize = '20') {
      record('usage', { view, filters, key, pageSize });
      const items =
        view === 'tasks'
          ? rows
          : rows
              .slice(0, 2)
              .map((r, i) => ({
                ...r,
                identity: 'fixture_call_' + i,
                charge: null,
                deducted: null,
                reserved: null,
                refunded: null,
              }));
      return {
        ...page(key ? items.slice(2) : items.slice(0, 2), key),
        summary: {
          scopeLabel: '筛选范围 · ',
          count: view === 'tasks' ? '4' : '2',
          deducted: '369',
          refunded: '160',
          knownCostCny: '1.320000123',
          hasUnknownCost: true,
          readAt: at,
        },
      } satisfies UsagePage;
    },
    async usageDetail(_view, identity) {
      record('usageDetail', identity);
      return detail(rows.find((r) => r.identity === identity) || rows[0]);
    },
  };
  return {
    service,
    requests,
    rows,
    get user() {
      return user;
    },
    set user(value: SessionView | null) {
      user = value;
    },
    get wallet() {
      return { ...wallet };
    },
    set wallet(value: CreditBalanceView) {
      wallet = value;
      balance = value.balance;
    },
    get policy() {
      return { ...policy };
    },
    set policy(value: CreditPolicyView) {
      policy = value;
    },
    get smtp() {
      return { ...smtp };
    },
    set smtp(value: SmtpSettingsDTO) {
      smtp = value;
    },
    setUncertain(v: boolean) {
      uncertain = v;
    },
  };
}
