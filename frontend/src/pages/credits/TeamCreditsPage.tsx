import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Navigate } from 'react-router-dom';
import { History } from 'lucide-react';
import { useOptionalSpace } from '../../state/SpaceContext';
import { useTeamCredits } from '../../features/credits/PersonalCredits';
import { useCreditRead } from '../../features/credits/useCreditRead';
import { holdsCredits, teamCreditsApi, type TeamLedger } from '../../api/team-credits';
import { useCatalogText } from '../../contexts/useCatalogText';
import { dateTime, formatInteger } from './credit-format';
import { allocationLabels, creditDirectionLabels, teamCreditTitle } from './personal-credit-format';
import { Button } from '../../components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from '../../components/ui/dialog';
import { toast } from '../../components/ui/sonner';
import { Choice, Feedback, Field, PageNavigation } from '../operations/controls';

export function teamCreditsPath(spaceId: string) {
  return '/team/' + encodeURIComponent(spaceId) + '?tab=credits';
}

// 团队的三余额、充值与团队流水只在团队页积分分区维护；/credits 对所有者只做转发，
// 对成员与管理员是“本人在团队中的积分”（名下分配额），版式沿用个人积分页。
export function TeamCreditsPage() {
  const space = useOptionalSpace();
  if (space?.current && !holdsCredits(space.current.role))
    return <Navigate replace to={teamCreditsPath(space.spaceId)} />;
  return <MemberCreditsPage />;
}

type RecordTab = 'usage' | 'allocation';
type Row = {
  key: string;
  at: string;
  title: string;
  detail: string;
  amount: string;
  direction: string;
  balance?: string;
};

function pageView(ledger: TeamLedger, tab: RecordTab, c: ReturnType<typeof useCatalogText>['c']) {
  const page = tab === 'usage' ? ledger : ledger.allocations;
  const total = page?.total ?? 0,
    current = page?.page ?? 1,
    size = page?.pageSize ?? 20;
  return {
    items: [] as unknown[],
    previous: current > 1 ? String(current - 1) : null,
    next: current * size < total ? String(current + 1) : null,
    caption: c('共 {total} 条 · 第 {page} 页', {
      total: total.toLocaleString('zh-CN'),
      page: current,
    }),
  };
}

function MemberCreditsPage() {
  const { c } = useCatalogText(),
    space = useOptionalSpace(),
    credits = useTeamCredits();
  const [tab, setTab] = useState<RecordTab>('usage'),
    [page, setPage] = useState(1),
    [direction, setDirection] = useState('all'),
    [range, setRange] = useState('all'),
    [requestKey, setRequestKey] = useState(0),
    [requestOpen, setRequestOpen] = useState(false);
  // 使用记录与分配记录同属一份成员流水回执，分页参数作用于当前页签对应的列表。
  const read = useCreditRead(
    () => teamCreditsApi.ledger(true, page, tab === 'usage' ? direction : 'all', range),
    [tab, page, direction, range, credits?.revision, space?.spaceId],
    Boolean(space?.ready),
  );
  const amount = credits?.balance == null ? '—' : formatInteger(credits.balance);
  const change = (apply: () => void) => {
    apply();
    setPage(1);
  };
  const rows: Row[] | null = !read.value
    ? null
    : tab === 'usage'
      ? read.value.items.map((item) => ({
          key: item.id,
          at: item.createdAt,
          title: teamCreditTitle(item, c),
          detail: c(creditDirectionLabels[item.kind]),
          amount: (['deduct', 'expire'].includes(item.kind) ? '−' : '+') + formatInteger(item.amount),
          direction: item.kind,
        }))
      : (read.value.allocations?.items ?? []).map((item) => ({
          key: item.id,
          at: item.createdAt,
          title: c(allocationLabels[item.kind]),
          detail:
            item.kind === 'allocate'
              ? c('由所有者或管理员分配')
              : item.kind === 'reclaim'
                ? c('由所有者或管理员收回')
                : c('退出团队时收回'),
          amount: (item.kind === 'allocate' ? '+' : '−') + formatInteger(item.amount),
          direction: item.kind === 'allocate' ? 'add' : 'deduct',
          balance: item.balance,
        }));
  const tabs: { value: RecordTab; label: string }[] = [
    { value: 'usage', label: c('使用记录') },
    { value: 'allocation', label: c('分配记录') },
  ];
  return (
    <section className="personal-credits-page">
      <div className="personal-credits-content">
        <header className="personal-credits-heading">
          <div>
            <div className="personal-credits-title">
              <h1>{c('积分明细')}</h1>
            </div>
            <p>
              {c('查看你在「{name}」的可用积分、使用与分配记录', {
                name: space?.current?.name || '',
              })}
            </p>
          </div>
        </header>
        <section
          className="personal-credit-overview"
          aria-labelledby="personal-credit-balance-title"
        >
          <div className="personal-credit-balance-card">
            <div>
              <h2 id="personal-credit-balance-title">{c('我的可用积分')}</h2>
              <div className="personal-credit-balance-value" data-long={amount.length > 14}>
                <strong>{amount}</strong>
                <span>{c('积分')}</span>
              </div>
            </div>
            <Button
              size="sm"
              disabled={!space?.ready}
              onClick={() => {
                setRequestKey((key) => key + 1);
                setRequestOpen(true);
              }}
            >
              {c('申请积分')}
            </Button>
          </div>
          {credits?.error ? (
            <div className="credit-panel-error" role="alert">
              <p>{credits.error}</p>
              <Button variant="ghost" size="sm" onClick={credits.refresh}>
                {c('重新读取余额')}
              </Button>
            </div>
          ) : null}
          {credits?.syncError ? (
            <p className="op-inline-note" role="status">
              {credits.syncError}
            </p>
          ) : null}
        </section>
        <section
          className="personal-credit-record-section team-member-credit-records"
          data-tab={tab}
          aria-labelledby="personal-credit-record-title"
        >
          <div className="personal-credit-record-heading">
            <h2 id="personal-credit-record-title">{c('积分记录')}</h2>
            <span className="personal-credit-update-status" role="status">
              {read.value && read.error ? c('显示上次读取结果') : null}
            </span>
          </div>
          <div className="personal-credit-toolbar">
            <div className="personal-credit-tabs" role="tablist" aria-label={c('积分记录类别')}>
              {tabs.map((item, index) => (
                <button
                  key={item.value}
                  id={'team-credit-record-tab-' + item.value}
                  type="button"
                  role="tab"
                  aria-selected={tab === item.value}
                  aria-controls="team-credit-records"
                  tabIndex={tab === item.value ? 0 : -1}
                  onClick={() => change(() => setTab(item.value))}
                  onKeyDown={(event) => {
                    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
                    event.preventDefault();
                    const next =
                      event.key === 'Home'
                        ? 0
                        : event.key === 'End'
                          ? tabs.length - 1
                          : (index + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) %
                            tabs.length;
                    change(() => setTab(tabs[next].value));
                    document
                      .getElementById('team-credit-record-tab-' + tabs[next].value)
                      ?.focus({ preventScroll: true });
                  }}
                >
                  {item.label}
                </button>
              ))}
            </div>
            <div className="personal-credit-filter-group">
              {tab === 'usage' ? (
                <Choice
                  label={c('记录类型')}
                  value={direction}
                  options={[
                    { value: 'all', label: c('全部') },
                    { value: 'consumed', label: c('已消耗') },
                    { value: 'acquired', label: c('已返还') },
                  ]}
                  onChange={(value) => change(() => setDirection(value))}
                />
              ) : null}
              <Choice
                label={c('积分记录时间')}
                value={range}
                options={[
                  { value: 'all', label: c('全部时间') },
                  { value: '7', label: c('最近 7 天') },
                  { value: '30', label: c('最近 30 天') },
                ]}
                onChange={(value) => change(() => setRange(value))}
              />
            </div>
          </div>
          <div
            className="personal-credit-record-card"
            role="tabpanel"
            id="team-credit-records"
            aria-labelledby={'team-credit-record-tab-' + tab}
            aria-busy={read.busy}
          >
            <Feedback busy={!read.value && read.busy} error={read.error} retry={read.refresh} />
            {read.value && rows ? (
              <>
                {rows.length ? (
                  <>
                    <div className="personal-credit-columns" aria-hidden>
                      <span>{c('时间')}</span>
                      <span>{c('变动说明')}</span>
                      <span>{c('积分变动')}</span>
                      {tab === 'allocation' ? <span>{c('变动后名下积分')}</span> : null}
                    </div>
                    <ul className="personal-credit-ledger" aria-label={c('积分明细')}>
                      {rows.map((row) => (
                        <li key={row.key}>
                          <time dateTime={row.at}>{dateTime(row.at)}</time>
                          <span className="personal-credit-reason">
                            <strong>{row.title}</strong>
                            <small>{row.detail}</small>
                          </span>
                          <span className="credit-record-amount" data-direction={row.direction}>
                            {row.amount}
                          </span>
                          {row.balance !== undefined ? (
                            <span className="personal-credit-after-balance">
                              <small>{c('变动后名下积分')}</small>
                              {formatInteger(row.balance)}
                            </span>
                          ) : null}
                        </li>
                      ))}
                    </ul>
                  </>
                ) : (
                  <div className="personal-credit-empty">
                    <History aria-hidden />
                    <h3>
                      {tab === 'usage'
                        ? c('当前条件下暂无使用记录')
                        : c('当前条件下暂无分配记录')}
                    </h3>
                    <p>
                      {tab === 'usage'
                        ? c('使用团队积分生成内容后，会在这里看到明细。')
                        : c('所有者或管理员为你分配或收回积分后，会在这里看到记录。')}
                    </p>
                  </div>
                )}
                <div className="credit-pagination">
                  <PageNavigation
                    page={pageView(read.value, tab, c)}
                    busy={read.busy}
                    onPage={(next) => setPage(Number(next))}
                  />
                </div>
              </>
            ) : null}
          </div>
        </section>
      </div>
      {space?.ready ? (
        <CreditRequestDialog
          key={requestKey}
          spaceId={space.spaceId}
          admin={space.current?.role === 'admin'}
          open={requestOpen}
          onClose={() => setRequestOpen(false)}
        />
      ) : null}
    </section>
  );
}

function CreditRequestDialog({
  spaceId,
  admin,
  open,
  onClose,
}: {
  spaceId: string;
  // 只有所有者能给管理员分配，管理员的申请只通知所有者。
  admin: boolean;
  open: boolean;
  onClose: () => void;
}) {
  const { c } = useCatalogText();
  const [needed, setNeeded] = useState(''),
    [busy, setBusy] = useState(false),
    [error, setError] = useState('');
  // 回执只归属发起它的这次弹窗；离页或换空间后迟到的结果不再更新界面。
  const alive = useRef(true);
  useEffect(
    () => () => {
      alive.current = false;
    },
    [],
  );
  const valid = /^\d+$/.test(needed) && BigInt(needed) > 0n;
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!valid || busy) return;
    setBusy(true);
    setError('');
    try {
      await teamCreditsApi.request(spaceId, needed);
      if (!alive.current) return;
      toast.success(c('积分申请通知已发送'));
      onClose();
    } catch (e) {
      if (alive.current) setError((e as Error).message);
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!value && !busy) onClose();
      }}
    >
      <DialogContent className="team-dialog" closeDisabled={busy}>
        <DialogTitle>{c('申请积分')}</DialogTitle>
        <DialogDescription>
          {admin
            ? c('填写本次所需积分，系统会通知团队所有者为你分配。')
            : c('填写本次所需积分，系统会通知团队所有者和管理员为你分配。')}
        </DialogDescription>
        <form className="team-form" onSubmit={submit}>
          <Field label={c('本次所需积分')}>
            <input
              type="number"
              min="1"
              step="1"
              inputMode="numeric"
              required
              autoFocus
              disabled={busy}
              value={needed}
              onChange={(e) => {
                setNeeded(e.target.value);
                setError('');
              }}
            />
          </Field>
          {error ? (
            <p className="op-feedback" role="alert">
              {error}
            </p>
          ) : null}
          <DialogFooter>
            <Button variant="outline" onClick={onClose} disabled={busy}>
              {c('取消')}
            </Button>
            <Button type="submit" busy={busy} disabled={!valid}>
              {admin ? c('通知所有者') : c('通知所有者和管理员')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
