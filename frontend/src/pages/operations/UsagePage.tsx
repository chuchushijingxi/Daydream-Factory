import { Fragment, useState, type FormEvent } from 'react';
import { ChevronDown, ChevronRight, Search, SlidersHorizontal } from 'lucide-react';
import { useSession } from '../../state/SessionContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useLanguage } from '../../contexts/LanguageContext';
import type {
  ChargeView,
  UsageFilters,
  UsageRow,
  UsageView,
  UsagePageSize,
} from '../../api/administration';
import { Button } from '../../components/ui/button';
import { ViewTabs } from '../../components/ui/view-tabs';
import { Choice, Feedback, Field, PageNavigation, useRead } from './controls';
import {
  chargeFormula,
  dateTime,
  formatCost,
  formatInteger,
  shanghaiInput,
  unitLabel,
} from '../credits/credit-format';
const emptyFilters: UsageFilters = {
  start: '',
  end: '',
  search: '',
  model: '',
  connection: '',
  business: '',
  result: '',
};
function Charge({ value }: { value: ChargeView | null }) {
  const { c } = useCatalogText();
  return value ? (
    <>
      <span>
        {formatInteger(value.quantity)} {unitLabel(value.unit, c)}
      </span>
      <small>
        {c('{price} {priceSuffix}', {
          price: formatInteger(value.unitPrice),
          priceSuffix:
            value.unit === 'characters'
              ? c('积分/100 字')
              : value.unit === 'images'
                ? c('积分/张')
                : c('积分/秒'),
        })}
      </small>
    </>
  ) : (
    <>—</>
  );
}
function UserCredits({ row }: { row: UsageRow }) {
  const { c } = useCatalogText();
  if (row.charge?.free)
    return (
      <>
        <span>
          0 <span className="op-tag">{c('限免')}</span>
        </span>
        <small>{c('原价 {amount} 积分', { amount: formatInteger(row.charge.original) })}</small>
      </>
    );
  return (
    <>
      {row.deducted !== null ? (
        <span>{c('{amount} 积分', { amount: formatInteger(row.deducted) })}</span>
      ) : row.reserved !== null ? (
        <span>{c('预扣 {amount} 积分', { amount: formatInteger(row.reserved) })}</span>
      ) : (
        <span>{c('待确认')}</span>
      )}
      {row.refunded !== null ? (
        <small>{c('已返还 {amount} 积分', { amount: formatInteger(row.refunded) })}</small>
      ) : null}
    </>
  );
}
function Detail({ view, row }: { view: UsageView; row: UsageRow }) {
  const { service } = useSession();
  const { c } = useCatalogText();
  const read = useRead(
    () => service.usageDetail(view, row.identity),
    [service, view, row.identity],
  );
  return (
    <div className="op-usage-detail">
      <Feedback busy={read.busy} error={read.error} retry={read.refresh} />
      {read.value ? (
        <>
          <h3>{c('计费与调用明细')}</h3>
          {read.value.charge ? (
            <p>
              {chargeFormula(read.value.charge, c)}
              {read.value.charge.unit === 'characters' ? c('（整笔向上取整）') : ''}
            </p>
          ) : null}
          {read.value.charge?.free ? (
            <p>
              {c('限免 0 积分 · 原价 {amount} 积分 · {start} 至 {end}', {
                amount: formatInteger(read.value.charge.original),
                start: dateTime(read.value.charge.freeStart),
                end: dateTime(read.value.charge.freeEnd),
              })}
            </p>
          ) : null}
          {read.value.safeError ? <p className="op-inline-error">{read.value.safeError}</p> : null}
          {read.value.calls.length ? (
            <div className="op-table-scroll" tabIndex={0} aria-label={c('关联调用')}>
              <table className="op-table op-nested-table">
                <thead>
                  <tr>
                    <th>{c('模型 / 连接')}</th>
                    <th>{c('输入 / 输出 Token')}</th>
                    <th>{c('执行')}</th>
                    <th>{c('平台成本')}</th>
                  </tr>
                </thead>
                <tbody>
                  {read.value.calls.map((call) => (
                    <tr key={call.identity}>
                      <td>
                        {call.modelLabel}
                        <small>{call.connectionLabel}</small>
                      </td>
                      <td>
                        {call.inputTokens === null ? c('待确认') : formatInteger(call.inputTokens)} /{' '}
                        {call.outputTokens === null ? c('待确认') : formatInteger(call.outputTokens)}
                      </td>
                      <td>{call.executionLabel}</td>
                      <td>{formatCost(call.costCny, c)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
          <p className="op-muted">{read.value.callCaption}</p>
        </>
      ) : null}
    </div>
  );
}
export function UsagePage() {
  const { service } = useSession();
  const { c } = useCatalogText();
  const { language } = useLanguage();
  const [view, setView] = useState<UsageView>('tasks'),
    [draft, setDraft] = useState(emptyFilters),
    [filters, setFilters] = useState(emptyFilters),
    [pages, setPages] = useState({ tasks: '', calls: '' }),
    [expanded, setExpanded] = useState(''),
    [advanced, setAdvanced] = useState(false),
    [formError, setFormError] = useState('');
  const [pageSize, setPageSize] = useState<UsagePageSize>('20');
  // 数据层已按请求时的语言生成本地化标签；切换语言时重新读取以保持一致。
  const options = useRead(() => service.usageOptions(), [service, language]);
  const read = useRead(
    () => service.usage(view, filters, pages[view], pageSize),
    [service, view, filters, pages[view], pageSize, language],
  );
  const patch = (key: keyof UsageFilters, value: string) =>
    setDraft((old) => ({ ...old, [key]: value }));
  const query = (e: FormEvent) => {
    e.preventDefault();
    try {
      if (draft.start && draft.end && draft.start >= draft.end)
        throw new Error(c('结束时间须晚于开始时间。'));
      setFilters({ ...draft, start: shanghaiInput(draft.start), end: shanghaiInput(draft.end) });
      setPages({ tasks: '', calls: '' });
      setExpanded('');
      setFormError('');
      read.refresh();
    } catch (e) {
      setFormError((e as Error).message);
    }
  };
  const all = (label: string, items: { value: string; label: string }[] | undefined) => [
    { value: '', label },
    ...(items || []),
  ];
  return (
    <section className="op-page op-usage-page">
      <header className="op-page-heading">
        <div>
          <h1>{c('模型使用记录')}</h1>
          <p>{c('查看用户任务、实际模型用量与平台成本。')}</p>
        </div>
      </header>
      <ViewTabs
        items={[
          { value: 'tasks', label: c('任务汇总') },
          { value: 'calls', label: c('调用明细') },
        ]}
        value={view}
        label={c('记录视角')}
        prefix="usage-view-"
        variant="line"
        panelId="usage-records"
        onChange={(value) => {
          setView(value as UsageView);
          setExpanded('');
        }}
      />
      <div
        className="op-view-panel"
        id="usage-records"
        role="tabpanel"
        aria-labelledby={'usage-view-' + view}
      >
        {view === 'calls' ? (
          <p className="op-view-explanation">
            {c(
              '每次实际模型外发单独记录，包含重试、Token、耗时及平台成本；不会重复收取用户积分。',
            )}
          </p>
        ) : null}
        {read.value ? (
          <div className="op-summary" aria-label={c('筛选统计')}>
            <span>
              {read.value.summary.scopeLabel}
              {view === 'tasks' ? c('任务') : c('调用')}
              <strong>{formatInteger(read.value.summary.count)}</strong>
            </span>
            {view === 'tasks' ? (
              <>
                <span>
                  {c('已实扣')}
                  <strong>
                    {formatInteger(read.value.summary.deducted)} <small>{c('积分')}</small>
                  </strong>
                </span>
                <span>
                  {c('已返还')}
                  <strong>
                    {formatInteger(read.value.summary.refunded)} <small>{c('积分')}</small>
                  </strong>
                </span>
              </>
            ) : (
              <span className="op-muted">{c('用户积分仅在所属任务结算一次')}</span>
            )}
            <span>
              {c('已知平台成本')}
              <strong>{formatCost(read.value.summary.knownCostCny, c)}</strong>
              {read.value.summary.hasUnknownCost ? <small>{c('另有待确认成本')}</small> : null}
            </span>
          </div>
        ) : null}
        <form className="op-usage-filters" onSubmit={query}>
          <div className="op-filters">
            <label className="op-search">
              <Search />
              <input
                aria-label={c('搜索用户、任务或请求 ID')}
                placeholder={c('搜索用户 / 任务 / 请求 ID')}
                value={draft.search}
                onChange={(e) => patch('search', e.target.value)}
              />
            </label>
            <Choice
              label={c('模型筛选')}
              value={draft.model}
              options={all(c('全部模型'), options.value?.models)}
              onChange={(v) => patch('model', v)}
            />
            <Choice
              label={c('执行结果筛选')}
              value={draft.result}
              options={all(c('全部执行结果'), options.value?.results)}
              onChange={(v) => patch('result', v)}
            />
            <Button
              variant="ghost"
              aria-expanded={advanced}
              aria-controls="usage-advanced"
              onClick={() => setAdvanced((v) => !v)}
            >
              <SlidersHorizontal />
              {c('更多筛选')}
            </Button>
            <Button type="submit">{c('查询')}</Button>
            <Button
              variant="outline"
              onClick={() => {
                setDraft(emptyFilters);
                setFilters(emptyFilters);
                setPages({ tasks: '', calls: '' });
                setExpanded('');
                setFormError('');
                read.refresh();
              }}
            >
              {c('重置')}
            </Button>
          </div>
          {advanced ? (
            <div id="usage-advanced" className="op-filters op-advanced">
              <Field label={c('开始时间（北京时间）')}>
                <input
                  type="datetime-local"
                  value={draft.start}
                  onChange={(e) => patch('start', e.target.value)}
                />
              </Field>
              <Field label={c('结束时间（不含）')}>
                <input
                  type="datetime-local"
                  value={draft.end}
                  onChange={(e) => patch('end', e.target.value)}
                />
              </Field>
              <Choice
                label={c('连接筛选')}
                value={draft.connection}
                options={all(c('全部连接'), options.value?.connections)}
                onChange={(v) => patch('connection', v)}
              />
              <Choice
                label={c('业务类型筛选')}
                value={draft.business}
                options={all(c('全部业务类型'), options.value?.businesses)}
                onChange={(v) => patch('business', v)}
              />
            </div>
          ) : null}
        </form>
        {formError ? (
          <p className="op-inline-error" role="alert">
            {formError}
          </p>
        ) : null}
        <Feedback busy={options.busy} error={options.error} retry={options.refresh} />
        <Feedback busy={read.busy} error={read.error} retry={read.refresh} />
        {read.value ? (
          <>
            <div
              className="op-table-scroll"
              tabIndex={0}
              aria-label={view === 'tasks' ? c('用户任务表格') : c('模型调用表格')}
            >
              <table className="op-table op-usage-table">
                <thead>
                  <tr>
                    {(view === 'tasks'
                      ? [
                          c('时间'),
                          c('用户'),
                          c('任务 / 模型'),
                          c('计费依据'),
                          c('用户积分'),
                          c('平台成本'),
                          c('执行 / 结算'),
                          c('详情'),
                        ]
                      : [
                          c('时间'),
                          c('用户'),
                          c('任务 / 模型 / 连接'),
                          c('实际用量'),
                          c('耗时'),
                          c('平台成本'),
                          c('执行'),
                          c('详情'),
                        ]
                    ).map((label) => (
                      <th key={label}>{label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {read.value.items.map((r) => (
                    <Fragment key={r.identity}>
                      <tr className={expanded === r.identity ? 'op-row-expanded' : ''}>
                        <td className="op-usage-time">
                          <time dateTime={r.at}>{dateTime(r.at)}</time>
                        </td>
                        <td className="op-usage-user">
                          <span title={r.userLabel}>{r.userLabel}</span>
                          {r.userEmail ? <small title={r.userEmail}>{r.userEmail}</small> : null}
                        </td>
                        <td>
                          <span title={r.modelLabel}>{r.operation}</span>
                          <small
                            className="op-model-label"
                            title={r.modelLabel + ' · ' + r.connectionLabel}
                          >
                            {r.modelLabel} · {r.connectionLabel}
                          </small>
                        </td>
                        <td>
                          {view === 'tasks' ? (
                            <Charge value={r.charge} />
                          ) : r.mediaUsageLabel ? (
                            <>
                              <span>{r.mediaUsageLabel}</span>
                            </>
                          ) : (
                            <>
                              {r.inputTokens === null
                                ? c('输入待确认')
                                : c('输入 {amount}', { amount: formatInteger(r.inputTokens) })}
                              <small>
                                {r.outputTokens === null
                                  ? c('输出待确认')
                                  : c('输出 {amount}', { amount: formatInteger(r.outputTokens) })}{' '}
                                Token
                              </small>
                            </>
                          )}
                        </td>
                        <td>
                          {view === 'tasks' ? (
                            <UserCredits row={r} />
                          ) : (
                            <>{r.durationLabel || '—'}</>
                          )}
                        </td>
                        <td className="op-numeric">
                          {formatCost(r.costCny, c)}
                          {r.costPending && r.costCny !== null ? (
                            <small>{c('另有待确认成本')}</small>
                          ) : null}
                        </td>
                        <td>
                          {r.executionLabel}
                          {view === 'tasks' ? <small>{r.settlementLabel}</small> : null}
                        </td>
                        <td>
                          <Button
                            size="sm"
                            variant="outline"
                            id={'usage-toggle-' + r.identity}
                            aria-label={c('详情 {operation} {user}', {
                              operation: r.operation,
                              user: r.userLabel,
                            })}
                            aria-expanded={expanded === r.identity}
                            aria-controls={'usage-detail-' + r.identity}
                            onClick={() =>
                              setExpanded((old) => (old === r.identity ? '' : r.identity))
                            }
                          >
                            {expanded === r.identity ? <ChevronDown /> : <ChevronRight />}
                            {c('详情')}
                          </Button>
                        </td>
                      </tr>
                      {expanded === r.identity ? (
                        <tr>
                          <td colSpan={8} id={'usage-detail-' + r.identity}>
                            <Detail view={view} row={r} />
                          </td>
                        </tr>
                      ) : null}
                    </Fragment>
                  ))}
                </tbody>
              </table>
            </div>
            <Feedback busy={false} error="" empty={!read.value.items.length} retry={read.refresh} />
            <div className="op-usage-pagination">
              <Choice
                label={c('每页条数')}
                value={pageSize}
                options={[
                  { value: '10', label: c('10 条 / 页') },
                  { value: '20', label: c('20 条 / 页') },
                  { value: '50', label: c('50 条 / 页') },
                ]}
                onChange={(value) => {
                  setPageSize(value as UsagePageSize);
                  setPages({ tasks: '', calls: '' });
                  setExpanded('');
                }}
              />
              <PageNavigation
                page={read.value}
                busy={read.busy}
                onPage={(p) => {
                  setPages((old) => ({ ...old, [view]: p }));
                  setExpanded('');
                }}
              />
            </div>
            <p className="op-muted op-read-time">
              {c('统计范围与当前视角、筛选一致 · 读取于 {time}', {
                time: dateTime(read.value.summary.readAt),
              })}
            </p>
          </>
        ) : null}
      </div>
    </section>
  );
}
