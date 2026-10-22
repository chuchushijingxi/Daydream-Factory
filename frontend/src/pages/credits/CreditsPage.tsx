import { useOptionalSpace } from '../../state/SpaceContext';
import { TeamCreditsPage } from './TeamCreditsPage';
import { useMemo, useState } from 'react';
import { History } from 'lucide-react';
import { useSession } from '../../state/SessionContext';
import type {
  CreditFilters,
  CreditPageSize,
  CreditLedgerPage,
  LedgerView,
  PageView,
} from '../../api/administration';
import { Choice, Feedback, PageNavigation } from '../operations/controls';
import { dateTime, formatInteger } from './credit-format';
import { usePersonalCredits } from '../../features/credits/PersonalCredits';
import { RechargeButton } from '../../features/payments/Recharge';
import {
  creditCategoryLabel,
  creditActorLabel,
  creditDirectionLabels,
  creditRecordTitle,
  creditSignedAmount,
  creditSourceLabel,
} from './personal-credit-format';
import { Button } from '../../components/ui/button';
import { useCreditRead } from '../../features/credits/useCreditRead';
import { useCatalogText } from '../../contexts/useCatalogText';
export function CreditPageSizeChoice({
  value,
  onChange,
  disabled,
}: {
  value: CreditPageSize;
  onChange: (value: CreditPageSize) => void;
  disabled: boolean;
}) {
  const { c } = useCatalogText();
  return (
    <Choice
      label={c('积分每页条数')}
      className="credit-page-size"
      value={value}
      disabled={disabled}
      options={['10', '20', '50'].map((size) => ({
        value: size,
        label: c('{size} 条 / 页', { size }),
      }))}
      onChange={(size) => onChange(size as CreditPageSize)}
    />
  );
}
export function Ledger({
  page,
  onPage,
  busy,
  pageSizeControl,
}: {
  pageSizeControl?: React.ReactNode;
  page: PageView<LedgerView>;
  onPage: (p: string) => void;
  busy: boolean;
}) {
  const { c } = useCatalogText();
  const labels = { add: '新增', deduct: '扣减', refund: '返还', expire: '到期回收', reclaim: '退款扣回' };
  return (
    <>
      <div className="op-table-scroll" tabIndex={0} aria-label={c('积分流水')}>
        <table className="op-table">
          <thead>
            <tr>
              {['时间', '类型 / 分类', '积分', '变更后余额', '来源', '原因'].map((x) => (
                <th key={x}>{c(x)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {page.items.map((x) => (
              <tr key={x.identity}>
                <td>{dateTime(x.at)}</td>
                <td>
                  {c(labels[x.direction])}
                  <small>{c(creditCategoryLabel(x.category))}</small>
                </td>
                <td className="op-numeric">
                  {['deduct', 'expire', 'reclaim'].includes(x.direction) ? '−' : '＋'}
                  {formatInteger(x.amount)}
                </td>
                <td className="op-numeric">{formatInteger(x.balance)}</td>
                <td>
                  {creditSourceLabel(x.source, c)}
                  {x.source.startsWith('payment-order:') && x.actorLabel.startsWith('payment:') ? (
                    <small>{creditActorLabel(x, c)}</small>
                  ) : null}
                </td>
                <td className="op-reason">{creditRecordTitle(x, c)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {page.items.length === 0 ? <p className="op-feedback">{c('暂无积分流水')}</p> : null}
      <div className="credit-pagination">
        {pageSizeControl}
        <PageNavigation page={page} busy={busy} onPage={onPage} />
      </div>
    </>
  );
}
const sourceLabels: Record<string, string> = {
  'language-generation': '文本生成',
  'media-generation': '图片与视频生成',
  'operations-adjustment': '运营扣减',
};
export function CreditAnalysis({
  value,
  busy = false,
}: {
  value: CreditLedgerPage;
  busy?: boolean;
}) {
  const { c } = useCatalogText();
  const total = BigInt(value.summary.settledConsumed);
  return (
    <section
      className="personal-credit-analysis"
      aria-labelledby="credit-analysis-title"
      aria-busy={busy}
    >
      <div className="personal-credit-analysis-heading">
        <h2 id="credit-analysis-title">{c('积分消耗分析')}</h2>
      </div>
      <div className="personal-credit-analysis-body">
        <div className="personal-credit-analysis-total">
          <span>{c('已结算消耗')}</span>
          <strong>
            {formatInteger(value.summary.settledConsumed)}
            <small>{c('积分')}</small>
          </strong>
          <p>{c('待结算预扣 {amount} 积分', { amount: formatInteger(value.summary.pending) })}</p>
        </div>
        {total === 0n ? (
          <div className="personal-credit-analysis-empty">{c('暂无已结算消耗')}</div>
        ) : (
          <ul aria-label={c('已结算消耗分布')}>
            {value.analysis
              .filter((row) => BigInt(row.amount) > 0n)
              .map((row) => (
                <li key={row.source}>
                  <div>
                    <span>{c(sourceLabels[row.source] || '其他消耗')}</span>
                    <strong>
                      {formatInteger(row.amount)} <small>{c('积分')}</small>
                    </strong>
                  </div>
                  <div className="personal-credit-analysis-track" aria-hidden>
                    <span
                      style={{ width: Number((BigInt(row.amount) * 10000n) / total) / 100 + '%' }}
                    />
                  </div>
                </li>
              ))}
          </ul>
        )}
      </div>
    </section>
  );
}
function rangeFilters(range: string) {
  if (range === 'all') return { start: '', end: '' };
  const now = Date.now(),
    day = 86400000;
  const start =
    range === 'month'
      ? new Date(new Date(now + 8 * 3600000).toISOString().slice(0, 7) + '-01T00:00:00+08:00')
      : new Date(now - Number(range) * day);
  return { start: start.toISOString(), end: new Date(now).toISOString() };
}
export function CreditsPage() {
  const space = useOptionalSpace();
  return space?.current?.kind === 'team' ? <TeamCreditsPage /> : <PersonalCreditsPage />;
}
function PersonalCreditsPage() {
  const { c } = useCatalogText();
  const { service, user } = useSession(),
    [page, setPage] = useState(''),
    [pageSize, setPageSize] = useState<CreditPageSize>('10'),
    [direction, setDirection] = useState<'acquired' | 'consumed' | 'expired' | 'reclaimed'>('acquired'),
    [category, setCategory] = useState<CreditFilters['category']>('all'),
    [range, setRange] = useState('all');
  const balance = usePersonalCredits();
  const scope = useMemo(
    () => ({ category, ...rangeFilters(range) }),
    [category, range, balance?.revision],
  );
  const filters = useMemo<CreditFilters>(() => ({ ...scope, direction }), [direction, scope]);
  const read = useCreditRead(
    () => service.myLedger(page, filters, pageSize),
    [service, page, user?.identity, filters, pageSize],
  );
  const analysis = useCreditRead(
    () => service.myLedger('', { ...scope, direction: 'consumed' }),
    [service, user?.identity, scope],
  );
  const amount = balance?.balance == null ? '—' : formatInteger(balance.balance),
    wallet = balance?.wallet;
  const change = (apply: () => void) => {
    apply();
    setPage('');
  };
  return (
    <section className="personal-credits-page">
      <div className="personal-credits-content">
        <header className="personal-credits-heading">
          <div>
            <div className="personal-credits-title">
              <h1>{c('积分明细')}</h1>
            </div>
            <p>{c('查看积分余额、获取记录与消耗情况')}</p>
          </div>
        </header>
        <section
          className="personal-credit-overview"
          aria-labelledby="personal-credit-balance-title"
        >
          <div className="personal-credit-balance-card">
            <div>
              <h2 id="personal-credit-balance-title">{c('当前可用积分')}</h2>
              <div className="personal-credit-balance-value" data-long={amount.length > 14}>
                <strong>{amount}</strong>
                <span>{c('积分')}</span>
              </div>
            </div>
            <RechargeButton />
          </div>
          <div className="personal-credit-categories">
            <article>
              <h3>{c('充值积分')}</h3>
              <strong>{wallet ? formatInteger(wallet.rechargeBalance) : '—'}</strong>
            </article>
            <article>
              <h3>{c('赠送及免费积分')}</h3>
              <strong>{wallet ? formatInteger(wallet.giftBalance) : '—'}</strong>
            </article>
            {wallet && BigInt(wallet.legacyBalance) > 0n ? (
              <article className="personal-credit-legacy">
                <h3>{c('历史未分类积分')}</h3>
                <strong>{formatInteger(wallet.legacyBalance)}</strong>
                <p>{c('保留历史余额，可正常使用')}</p>
              </article>
            ) : null}
          </div>
          {balance?.error ? (
            <div className="credit-panel-error" role="alert">
              <p>{balance.error}</p>
              <Button variant="ghost" size="sm" onClick={balance.refresh}>
                {c('重新读取余额')}
              </Button>
            </div>
          ) : null}
          {balance?.syncError ? (
            <p className="op-inline-note" role="status">
              {balance.syncError}
            </p>
          ) : null}
        </section>
        <section
          className="personal-credit-record-section"
          aria-labelledby="personal-credit-record-title"
        >
          <div className="personal-credit-record-heading">
            <h2 id="personal-credit-record-title">{c('积分记录')}</h2>
            <span className="personal-credit-update-status" role="status">
              {read.value && read.error ? c('显示上次读取结果') : null}
            </span>
          </div>
          <div className="personal-credit-toolbar">
            <div className="personal-credit-tabs" role="tablist" aria-label={c('积分记录方向')}>
              {(
                [
                  { value: 'acquired', label: c('已获取') },
                  { value: 'consumed', label: c('已消耗') },
                  { value: 'expired', label: c('到期回收') },
                  { value: 'reclaimed', label: c('退款扣回') },
                ] as const
              ).map((tab, index) => (
                <button
                  type="button"
                  role="tab"
                  id={'credit-record-tab-' + tab.value}
                  key={tab.value}
                  aria-selected={direction === tab.value}
                  aria-controls="personal-credit-records"
                  tabIndex={direction === tab.value ? 0 : -1}
                  onClick={() => change(() => setDirection(tab.value))}
                  onKeyDown={(event) => {
                    const keys = ['ArrowLeft', 'ArrowRight', 'Home', 'End'];
                    if (!keys.includes(event.key)) return;
                    event.preventDefault();
                    const choices = ['acquired', 'consumed', 'expired', 'reclaimed'] as const;
                    const next =
                      event.key === 'Home'
                        ? 0
                        : event.key === 'End'
                          ? choices.length - 1
                          : (index + (event.key === 'ArrowRight' ? 1 : -1) + choices.length) %
                            choices.length;
                    change(() => setDirection(choices[next]));
                    document
                      .getElementById('credit-record-tab-' + choices[next])
                      ?.focus({ preventScroll: true });
                  }}
                >
                  {tab.label}
                </button>
              ))}
            </div>
            <div className="personal-credit-filter-group">
              <Choice
                label={c('积分分类')}
                value={category}
                options={[
                  { value: 'all', label: c('全部积分') },
                  { value: 'recharge', label: c('充值积分') },
                  { value: 'gift', label: c('赠送及免费积分') },
                  ...((wallet && BigInt(wallet.legacyBalance) > 0n) ||
                  read.value?.items.some(
                    (entry) => entry.legacyAmount && BigInt(entry.legacyAmount) > 0n,
                  )
                    ? [{ value: 'legacy', label: c('历史未分类积分') }]
                    : []),
                ]}
                onChange={(value) => change(() => setCategory(value as CreditFilters['category']))}
              />
              <Choice
                label={c('积分记录时间')}
                value={range}
                options={[
                  { value: 'all', label: c('全部时间') },
                  { value: '7', label: c('最近 7 天') },
                  { value: '30', label: c('最近 30 天') },
                  { value: 'month', label: c('本月') },
                ]}
                onChange={(value) => change(() => setRange(value))}
              />
            </div>
          </div>
          <div
            className="personal-credit-record-card"
            role="tabpanel"
            id="personal-credit-records"
            aria-labelledby={'credit-record-tab-' + direction}
            aria-busy={read.busy}
          >
            <Feedback busy={!read.value && read.busy} error={read.error} retry={read.refresh} />
            {read.value ? (
              <>
                {read.value.items.length ? (
                  <>
                    <div className="personal-credit-columns" aria-hidden>
                      <span>{c('时间')}</span>
                      <span>{c('变动说明')}</span>
                      <span>{c('积分变动')}</span>
                      <span>{c('变动后余额')}</span>
                    </div>
                    <ul className="personal-credit-ledger" aria-label={c('积分明细')}>
                      {read.value.items.map((entry) => (
                        <li key={entry.identity}>
                          <time dateTime={entry.at}>{dateTime(entry.at)}</time>
                          <span className="personal-credit-reason">
                            <strong>{creditRecordTitle(entry, c)}</strong>
                            <small>
                              {c(creditDirectionLabels[entry.direction])} ·{' '}
                              {c(creditCategoryLabel(entry.category))}
                            </small>
                          </span>
                          <span className="credit-record-amount" data-direction={entry.direction}>
                            {creditSignedAmount(entry)}
                          </span>
                          <span className="personal-credit-after-balance">
                            <small>{c('变动后余额')}</small>
                            {formatInteger(entry.balance)}
                          </span>
                        </li>
                      ))}
                    </ul>
                  </>
                ) : (
                  <div className="personal-credit-empty">
                    <History aria-hidden />
                    <h3>{c('当前条件下暂无积分记录')}</h3>
                    <p>{c('调整筛选条件，或在积分变动后查看明细。')}</p>
                  </div>
                )}
                <div className="credit-pagination">
                  <CreditPageSizeChoice
                    value={pageSize}
                    disabled={read.busy}
                    onChange={(value) => change(() => setPageSize(value))}
                  />
                  <PageNavigation page={read.value} busy={read.busy} onPage={setPage} />
                </div>
              </>
            ) : null}
          </div>
        </section>
        <Feedback
          busy={!analysis.value && analysis.busy}
          error={analysis.error}
          retry={analysis.refresh}
        />
        {analysis.error && analysis.value ? (
          <p className="op-muted">{c('分析保留上次读取结果。')}</p>
        ) : null}
        {analysis.value ? <CreditAnalysis value={analysis.value} busy={analysis.busy} /> : null}
      </div>
    </section>
  );
}
