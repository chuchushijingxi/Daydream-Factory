import { ApiError, http } from './client';
import { safeInteger, creditInput } from './administration';
import { translate } from '../contexts/translate';
export type TeamBalance = {
  spaceId: string;
  role: 'owner' | 'admin' | 'member';
  balance: string;
  totalBalance?: string;
  allocated?: string;
  unallocated?: string;
};
export type TeamCreditEntry = {
  id: string;
  kind: 'add' | 'deduct' | 'refund' | 'expire';
  amount: string;
  balance?: string;
  reason: string;
  createdAt: string;
};
export type AllocationEntry = {
  id: string;
  kind: 'allocate' | 'reclaim' | 'release';
  amount: string;
  balance: string;
  createdAt: string;
};
export type TeamLedger = {
  items: TeamCreditEntry[];
  total: number;
  page: number;
  pageSize: number;
  allocations?: { items: AllocationEntry[]; total: number; page: number; pageSize: number };
};
const invalid = () => new ApiError(200, 'PROGRAM_FAILED', translate('团队积分回执不完整。'));
export function teamBalanceView(raw: unknown): TeamBalance {
  const v = raw as Record<string, unknown>;
  if (
    !v ||
    typeof v.spaceId !== 'string' ||
    !v.spaceId ||
    !['owner', 'admin', 'member'].includes(String(v.role))
  )
    throw invalid();
  const out: TeamBalance = {
    spaceId: v.spaceId,
    role: v.role as TeamBalance['role'],
    balance: safeInteger(v.balance as number),
  };
  if (out.role === 'member') {
    if (['totalBalance', 'allocated', 'unallocated'].some((k) => k in v)) throw invalid();
  } else {
    out.totalBalance = safeInteger(v.totalBalance as number);
    out.allocated = safeInteger(v.allocated as number);
    out.unallocated = safeInteger(v.unallocated as number);
    // 所有者的 balance 是团队总额；管理员的 balance 是本人名下分配额，必然含在已分配之内。
    if (
      BigInt(out.totalBalance) !== BigInt(out.allocated) + BigInt(out.unallocated) ||
      (out.role === 'owner'
        ? out.balance !== out.totalBalance
        : BigInt(out.balance) > BigInt(out.allocated))
    )
      throw invalid();
  }
  return out;
}
// 成员与管理员只花本人名下分配额；所有者直接使用未分配积分。
export const holdsCredits = (role: TeamBalance['role'] | undefined) =>
  role === 'member' || role === 'admin';
function allocation(raw: unknown): AllocationEntry {
  const v = raw as Record<string, unknown>;
  if (
    !v ||
    typeof v.id !== 'string' ||
    !['allocate', 'reclaim', 'release'].includes(String(v.kind)) ||
    typeof v.createdAt !== 'string' ||
    !Number.isFinite(Date.parse(v.createdAt))
  )
    throw invalid();
  return {
    id: v.id,
    kind: v.kind as AllocationEntry['kind'],
    amount: safeInteger(v.amount as number),
    balance: safeInteger(v.balance as number),
    createdAt: v.createdAt,
  };
}
export function teamLedgerView(raw: unknown, member: boolean): TeamLedger {
  const v = raw as {
    items: Record<string, unknown>[];
    total: number;
    page: number;
    pageSize: number;
    allocations?: { items: unknown[]; total: number; page: number; pageSize: number };
  };
  if (
    !v ||
    !Array.isArray(v.items) ||
    [v.total, v.page, v.pageSize].some((n) => !Number.isSafeInteger(n) || n < 0)
  )
    throw invalid();
  const items = v.items.map((x) => {
    if (
      typeof x.id !== 'string' ||
      !['add', 'deduct', 'refund', 'expire'].includes(String(x.kind)) ||
      typeof x.reason !== 'string' ||
      typeof x.createdAt !== 'string' ||
      !Number.isFinite(Date.parse(x.createdAt)) ||
      (member && 'balance' in x)
    )
      throw invalid();
    return {
      id: x.id,
      kind: x.kind as TeamCreditEntry['kind'],
      amount: safeInteger(x.amount as number),
      ...(member ? {} : { balance: safeInteger(x.balance as number) }),
      reason: x.reason,
      createdAt: x.createdAt,
    };
  });
  const out: TeamLedger = { items, total: v.total, page: v.page, pageSize: v.pageSize };
  if (v.allocations) {
    if (
      !Array.isArray(v.allocations.items) ||
      [v.allocations.total, v.allocations.page, v.allocations.pageSize].some(
        (n) => !Number.isSafeInteger(n) || n < 0,
      )
    )
      throw invalid();
    out.allocations = { ...v.allocations, items: v.allocations.items.map(allocation) };
  }
  return out;
}
export const teamCreditsApi = {
  async balance() {
    return teamBalanceView(await http.json('/api/credits/balance'));
  },
  // holder=true 读本人名下流水（成员或管理员的积分明细）；false 读团队页的团队流水。
  async ledger(holder: boolean, page = 1, direction = 'all', range = 'all') {
    const params = new URLSearchParams({
      page: String(page),
      pageSize: '20',
      direction,
    });
    if (holder) params.set('scope', 'holder');
    if (range !== 'all')
      params.set('start', new Date(Date.now() - Number(range) * 86400000).toISOString());
    return teamLedgerView(await http.json('/api/credits/ledger?' + params), holder);
  },
  async recent(member: boolean) {
    const raw = await http.json<{ items: unknown[]; allocations?: unknown[] }>(
      '/api/credits/ledger/recent',
    );
    if (
      !raw ||
      !Array.isArray(raw.items) ||
      raw.items.length > 5 ||
      (raw.allocations && (!Array.isArray(raw.allocations) || raw.allocations.length > 5))
    )
      throw invalid();
    return {
      items: teamLedgerView(
        { items: raw.items, total: raw.items.length, page: 1, pageSize: 5 },
        member,
      ).items,
      allocations: raw.allocations?.map(allocation) ?? [],
    };
  },
  async request(spaceId: string, needed: string) {
    const value = Number(creditInput(needed));
    const v = await http.json<{ requested: boolean }>(
      '/api/spaces/' + encodeURIComponent(spaceId) + '/credit-requests',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ needed: value }),
      },
    );
    if (v?.requested !== true) throw invalid();
  },
};
