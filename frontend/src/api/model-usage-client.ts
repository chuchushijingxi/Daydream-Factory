import { responseFields } from './response-fields';
import { ApiError, type createHttpClient } from './client';
import { translate } from '../contexts/translate';
import { pageView, safeInteger } from './response-values';
import type {
  ChargeView,
  UsageFilters,
  UsagePage,
  UsagePageSize,
  UsageRow,
  UsageView,
} from './administration';
import type { ModelConfiguration } from './types';
import type { UsageBasisDTO, UsageRecordDTO, UsageResultDTO } from './administration-types';
// 代码→中文源文案的展示映射；值只在 row()/usageOptions() 事件时经 translate
// 本地化，不在模块顶层翻译，避免语言被冻结。
const operationLabels: Record<string, string> = {
  'script-parse': '整剧解析',
  'asset-preparation-v1': '整剧解析',
  'catalog-agent-v3': '资产提取',
  'visual-prompt-agent-v1': '提示词生成',
  'storyboard-agent-v1': '分集制作',
  'episode-index-agent-v1': '分集解析',
  'image.generate': '图片生成',
  'video.generate': '视频生成',
};
const statusLabels: Record<string, string> = {
  running: '执行中',
  queued: '排队中',
  completed: '成功',
  succeeded: '成功',
  failed: '失败',
  cancelled: '已取消',
  unknown: '待确认',
  applied: '已采用',
  accepted: '已受理',
  immediate: '即时回执',
  rejected: '已拒绝',
  may_have_sent: '提交待确认',
};
const settlementLabels: Record<string, string> = {
  pending: '未结算',
  'refund-pending': '待返还',
  refunded: '已返还',
  settled: '已结算',
  'task-billing': '仅所属任务计费',
};
// 已知状态映射在事件时翻译；映射外的服务端代码保持原样，绝不翻译未知文本。
const localizedLabel = (labels: Record<string, string>, code: string) => {
  const label = labels[code];
  return label ? translate(label) : code;
};
const usageFields: Record<keyof UsageRecordDTO, true> = {
  spaceId: true,
  spaceName: true,
  spaceKind: true,
  id: true,
  taskId: true,
  userId: true,
  username: true,
  email: true,
  operation: true,
  status: true,
  modelId: true,
  connectionId: true,
  requestId: true,
  createdAt: true,
  durationMs: true,
  inputTokens: true,
  outputTokens: true,
  cacheHitTokens: true,
  mediaUsage: true,
  knownCostMicroCny: true,
  unknownCost: true,
  callCount: true,
  basis: true,
  prepaid: true,
  refunded: true,
  charged: true,
  settlement: true,
  deductionId: true,
  refundId: true,
  errorCode: true,
};
function projection(v: UsageRecordDTO) {
  v = responseFields(v, Object.keys(usageFields) as (keyof UsageRecordDTO)[]);
  if (
    typeof v.unknownCost !== 'boolean' ||
    typeof v.id !== 'string' ||
    !v.id ||
    typeof v.userId !== 'string' ||
    (v.username !== null && typeof v.username !== 'string') ||
    (v.email !== null && typeof v.email !== 'string')
  )
    throw new ApiError(200, 'PROGRAM_FAILED', translate('使用记录身份或费用状态回执不完整。'));
  if (
    (v.spaceId !== undefined && typeof v.spaceId !== 'string') ||
    (v.spaceName !== undefined && typeof v.spaceName !== 'string') ||
    (v.spaceKind !== undefined && v.spaceKind !== 'personal' && v.spaceKind !== 'team')
  )
    throw new ApiError(
      200,
      'CONTENT_BOUNDARY_VIOLATION',
      translate('使用记录空间信息不符合安全投影合同。'),
    );
  if (v.mediaUsage) {
    v.mediaUsage = responseFields(v.mediaUsage, ['imageCount', 'videoSeconds']);
    if (
      !Number.isSafeInteger(v.mediaUsage.imageCount) ||
      v.mediaUsage.imageCount < 0 ||
      (v.mediaUsage.videoSeconds !== null &&
        (!Number.isFinite(v.mediaUsage.videoSeconds) ||
          v.mediaUsage.videoSeconds < 0 ||
          v.mediaUsage.videoSeconds > Number.MAX_SAFE_INTEGER))
    )
      throw new ApiError(
        200,
        'CONTENT_BOUNDARY_VIOLATION',
        translate('媒体用量不符合服务端窄投影合同，请后端核查。'),
      );
  }
  if (v.basis) v.basis = validateChargeBasis(v.basis);
  return v;
}
export function microCny(value: number) {
  const digits = safeInteger(value).padStart(7, '0');
  return digits.slice(0, -6) + '.' + digits.slice(-6);
}
export function validateChargeBasis(v: UsageBasisDTO) {
  const keys = [
    'sourceVersion',
    'inputRevision',
    'unit',
    'id',
    'modelId',
    'connectionId',
    'scope',
    'scopeDigest',
    'quantity',
    'priceVersion',
    'rule',
    'acceptedAt',
    'originalAmount',
    'amount',
    'free',
  ] as const;
  v = responseFields(v, keys);
  v.rule = responseFields(v.rule, ['unitPrice', 'freeEnabled', 'freeStart', 'freeEnd']);
  if (
    !['characters', 'image', 'second'].includes(v.unit) ||
    typeof v.free !== 'boolean' ||
    typeof v.rule.freeEnabled !== 'boolean'
  )
    throw new ApiError(
      200,
      'CONTENT_BOUNDARY_VIOLATION',
      translate('计费依据返回了合同外内容，请后端修复。'),
    );
  for (const n of [v.sourceVersion, v.inputRevision, v.priceVersion]) safeInteger(n);
  chargeView(v);
  return v;
}
export function chargeView(v: NonNullable<UsageRecordDTO['basis']>): ChargeView {
  const unit = ({ characters: 'characters', image: 'images', second: 'seconds' } as const)[v.unit];
  if (!unit) throw new ApiError(200, 'PROGRAM_FAILED', translate('计费单位回执无效。'));
  return {
    quantity: safeInteger(v.quantity),
    unit,
    unitPrice: safeInteger(v.rule.unitPrice),
    original: safeInteger(v.originalAmount),
    due: safeInteger(v.amount),
    free: v.free,
    freeStart: v.rule.freeStart,
    freeEnd: v.rule.freeEnd,
  };
}
function row(v: UsageRecordDTO, view: UsageView): UsageRow {
  projection(v);
  const unsettled = v.settlement === 'pending' || v.settlement === 'refund-pending';
  return {
    identity: v.id,
    spaceId: v.spaceId,
    spaceName: v.spaceName,
    spaceKind: v.spaceKind,
    at: v.createdAt,
    userLabel: v.username || translate(v.userId ? '未记录用户名' : '未记录用户'),
    userEmail: v.email,
    userIdentity: v.userId,
    operation: localizedLabel(operationLabels, v.operation),
    modelLabel: v.modelId,
    connectionLabel: v.connectionId || '—',
    executionLabel: localizedLabel(statusLabels, v.status),
    settlementLabel: localizedLabel(settlementLabels, v.settlement),
    charge: v.basis ? chargeView(v.basis) : null,
    deducted: view === 'calls' || unsettled ? null : safeInteger(v.charged),
    reserved: view === 'tasks' && unsettled ? safeInteger(v.prepaid) : null,
    refunded: view === 'tasks' && v.refundId ? safeInteger(v.refunded) : null,
    costCny: v.unknownCost && v.knownCostMicroCny === 0 ? null : microCny(v.knownCostMicroCny),
    costPending: v.unknownCost,
    taskIdentity: v.taskId,
    inputTokens: v.inputTokens === null ? null : safeInteger(v.inputTokens),
    outputTokens: v.outputTokens === null ? null : safeInteger(v.outputTokens),
    mediaUsageLabel: v.mediaUsage
      ? (v.mediaUsage.imageCount
          ? translate('{count} 张', { count: safeInteger(v.mediaUsage.imageCount) })
          : translate('视频')) +
        (v.mediaUsage.videoSeconds !== null
          ? translate(' · 实际 {seconds} 秒', { seconds: v.mediaUsage.videoSeconds })
          : '')
      : null,
    durationLabel: v.durationMs === null ? null : safeInteger(v.durationMs) + ' ms',
    requestIdentity: v.requestId,
  };
}
export function createUsageApi(transport: ReturnType<typeof createHttpClient>) {
  async function query(
    view: UsageView,
    filters: UsageFilters,
    page = '',
    pageSize: UsagePageSize = '20',
  ) {
    const q = new URLSearchParams({ view, page: page || '1', pageSize, sort: 'desc' });
    for (const [key, value] of Object.entries({
      from: filters.start,
      until: filters.end,
      [filters.searchBy || 'q']: filters.search.trim(),
      spaceId: filters.spaceId || '',
      modelId: filters.model,
      connectionId: filters.connection,
      operation: filters.business,
      status: filters.result,
    }))
      if (value) q.set(key, value);
    const v = responseFields(
      await transport.json<UsageResultDTO>('/api/operations/model-usage?' + q),
      ['items', 'total', 'page', 'pageSize', 'stats', 'readAt'],
    );
    v.stats = responseFields(v.stats, [
      'count',
      'callCount',
      'charged',
      'refunded',
      'knownCostMicroCny',
      'unknownCost',
    ]);
    if (!Array.isArray(v.items) || typeof v.stats.unknownCost !== 'boolean')
      throw new ApiError(200, 'PROGRAM_FAILED', translate('使用记录统计回执不完整。'));
    v.items = v.items.map(projection);
    return v;
  }
  return {
    async usageOptions() {
      // Existing operations metadata only; never fetch cross-user project/call bodies.
      const v = await transport.json<ModelConfiguration>('/api/model-config');
      return {
        models: v.connections.flatMap((c) =>
          c.models.map((m) => ({ value: m.id, label: m.name + ' · ' + c.name })),
        ),
        connections: v.connections.map((c) => ({ value: c.id, label: c.name })),
        businesses: Object.entries(operationLabels).map(([value, label]) => ({
          value,
          label: translate(label),
        })),
        results: Object.entries(statusLabels).map(([value, label]) => ({
          value,
          label: translate(label),
        })),
      };
    },
    async usage(
      view: UsageView,
      filters: UsageFilters,
      page?: string,
      pageSize?: UsagePageSize,
    ): Promise<UsagePage> {
      const v = await query(view, filters, page, pageSize);
      return {
        ...pageView(v, (x) => row(x, view)),
        summary: {
          scopeLabel: translate('筛选范围 · '),
          count: safeInteger(v.stats.count),
          deducted: safeInteger(v.stats.charged),
          refunded: safeInteger(v.stats.refunded),
          knownCostCny: microCny(v.stats.knownCostMicroCny),
          hasUnknownCost: v.stats.unknownCost,
          readAt: v.readAt,
        },
      };
    },
    async usageDetail(view: UsageView, identity: string) {
      const empty: UsageFilters = {
        start: '',
        end: '',
        search: identity,
        searchBy: view === 'tasks' ? 'taskId' : 'requestId',
        model: '',
        connection: '',
        business: '',
        result: '',
      };
      const original = await query(view, empty);
      const item = original.items.find((x) => x.id === identity);
      if (!item) throw new ApiError(404, 'NOT_FOUND', translate('未找到当前记录。'));
      const taskFilter = { ...empty, search: item.taskId, searchBy: 'taskId' as const };
      const [tasks, calls] = await Promise.all([
        view === 'tasks' ? Promise.resolve(original) : query('tasks', taskFilter),
        query('calls', taskFilter),
      ]);
      const task = tasks.items.find((x) => x.taskId === item.taskId);
      if (!task) throw new ApiError(404, 'NOT_FOUND', translate('未找到关联任务计费依据。'));
      return {
        charge: task.basis ? chargeView(task.basis) : null,
        safeError: task.errorCode,
        callCaption: translate('本页 {page} 次 / 共 {total} 次调用', {
          page: calls.items.length,
          total: safeInteger(calls.total),
        }),
        calls: calls.items.map((c) => row(c, 'calls')),
      };
    },
  };
}
