import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Search, List, SlidersHorizontal } from 'lucide-react';
import { useSession } from '../../state/SessionContext';
import { sessionBoundary } from '../../api/session-boundary';
import { ApiError } from '../../api/http-error';
import {
  creditInput,
  type Adjustment,
  type UserView,
  type CreditPageSize,
} from '../../api/administration';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '../../components/ui/dialog';
import {
  Feedback,
  Field,
  PageNavigation,
  useRead,
  Choice,
  StatusBadge,
} from '../operations/controls';
import { formatInteger, integer } from './credit-format';
import { Ledger, CreditPageSizeChoice } from './CreditsPage';
import { useCreditUpdates } from '../../features/credits/useCreditUpdates';
import { useCatalogText } from '../../contexts/useCatalogText';

type Account = { identity: string };
export function OperationsCreditsPage() {
  const { c } = useCatalogText();
  const { service, user } = useSession();
  const [search, setSearch] = useState(''),
    [query, setQuery] = useState(''),
    [page, setPage] = useState('');
  const users = useRead(() => service.users(query, page), [service, query, page, user?.username]);
  const [selected, setSelected] = useState<Account | null>(null),
    [mode, setMode] = useState<'adjust' | 'ledger'>('adjust'),
    [open, setOpen] = useState(false),
    // 先读余额与流水再打开弹窗，避免弹窗从加载态突然撑开（闪一下）。
    [opening, setOpening] = useState(false),
    [ledgerPage, setLedgerPage] = useState(''),
    [ledgerPageSize, setLedgerPageSize] = useState<CreditPageSize>('10');
  const [direction, setDirection] = useState<'add' | 'deduct'>('add'),
    [category, setCategory] = useState<'recharge' | 'gift'>('gift'),
    [amount, setAmount] = useState(''),
    [reason, setReason] = useState(''),
    [pending, setPending] = useState<Adjustment | null>(null),
    [error, setError] = useState(''),
    [notice, setNotice] = useState(''),
    [busy, setBusy] = useState(false),
    [absent, setAbsent] = useState(false);
  const guard = useRef(false),
    epoch = sessionBoundary.epoch,
    storageKey = 'drama:credit-command:' + user?.identity;
  useEffect(() => {
    try {
      const saved = sessionStorage.getItem(storageKey),
        restored: Adjustment | null = saved ? JSON.parse(saved) : null;
      setPending(restored);
      if (restored) {
        setDirection(restored.direction);
        setCategory(restored.category || 'gift');
        setAmount(restored.amount);
        setReason(restored.reason);
      }
    } catch {
      setError(c('无法读取待核对操作，请通过流水核对历史提交。'));
    }
  }, [storageKey]);
  const updates = useCreditUpdates(open && !!selected, selected?.identity ?? null);
  const wallet = useRead(
    () => service.userWallet(selected!.identity, ledgerPage, ledgerPageSize),
    [service, selected?.identity, ledgerPage, ledgerPageSize, updates.revision],
    !!selected && (open || opening),
  );
  // 实时同步或翻页回读期间保留同一账号上次确认的余额，读到新值再替换，不回到加载态。
  const [shownWallet, setShownWallet] = useState<{
    identity: string;
    value: NonNullable<typeof wallet.value>;
  } | null>(null);
  useEffect(() => {
    if (wallet.value && selected) setShownWallet({ identity: selected.identity, value: wallet.value });
  }, [wallet.value, selected?.identity]);
  const walletValue =
    wallet.value ??
    (shownWallet && selected && shownWallet.identity === selected.identity ? shownWallet.value : null);
  useEffect(() => {
    if (!opening) return;
    if (wallet.value || wallet.error) {
      setOpening(false);
      setOpen(true);
      return;
    }
    // 读取较慢时不让运营干等，超时后照常打开并显示加载。
    const timer = setTimeout(() => {
      setOpening(false);
      setOpen(true);
    }, 400);
    return () => clearTimeout(timer);
  }, [opening, wallet.value, wallet.error]);
  const remember = (value: Adjustment | null) => {
    setPending(value);
    try {
      if (value) sessionStorage.setItem(storageKey, JSON.stringify(value));
      else sessionStorage.removeItem(storageKey);
    } catch {
      setError(c('浏览器无法保存操作编号，请在本页核对提交结果。'));
    }
  };
  const show = (account: Account, nextMode: 'adjust' | 'ledger') => {
    if (busy) return;
    setSelected(account);
    setMode(nextMode);
    setLedgerPage('');
    setError('');
    setNotice('');
    if (!pending) {
      setDirection('add');
      setCategory('gift');
      setAmount('');
      setReason('');
      setAbsent(false);
    }
    setShownWallet(null);
    setOpening(true);
  };
  const showUser = (account: UserView, nextMode: 'adjust' | 'ledger') =>
    show({ identity: account.identity }, nextMode);
  const adjust = async (event: FormEvent) => {
    event.preventDefault();
    if (guard.current || pending || !selected) return;
    let parsed: string;
    try {
      parsed = integer(amount, true);
      creditInput(parsed);
    } catch (e) {
      setError((e as Error).message);
      return;
    }
    if (!reason.trim()) {
      setError(c('请填写调整原因。'));
      return;
    }
    const command: Adjustment = {
      command: crypto.randomUUID(),
      account: selected.identity,
      direction,
      amount: parsed,
      reason: reason.trim(),
      ...(direction === 'add' ? { category } : {}),
    };
    guard.current = true;
    setBusy(true);
    setError('');
    setNotice('');
    setAbsent(false);
    remember(command);
    try {
      const result = await service.adjust(command);
      sessionBoundary.assert(epoch);
      if (result.state === 'committed') {
        remember(null);
        setAmount('');
        setReason('');
        setNotice(c('已确认记账，流水编号 {id}', { id: result.entry.identity }));
        wallet.refresh();
      } else setNotice(result.message);
    } catch (e) {
      if (epoch === sessionBoundary.epoch) {
        setError((e as Error).message);
        if (e instanceof ApiError && e.status >= 400 && e.status < 500) remember(null);
      }
    } finally {
      guard.current = false;
      if (epoch === sessionBoundary.epoch) setBusy(false);
    }
  };
  const reconcile = async () => {
    if (!pending || guard.current) return;
    guard.current = true;
    setBusy(true);
    setError('');
    try {
      const result = await service.adjustment(pending.command);
      sessionBoundary.assert(epoch);
      if (result.state === 'committed') {
        remember(null);
        setNotice(c('已回读确认记账：{id}', { id: result.entry.identity }));
        wallet.refresh();
      } else {
        setNotice(result.message);
        setAbsent(result.state === 'absent');
      }
    } catch (e) {
      if (epoch === sessionBoundary.epoch) setError((e as Error).message);
    } finally {
      guard.current = false;
      if (epoch === sessionBoundary.epoch) setBusy(false);
    }
  };
  const retryOriginal = async () => {
    if (!pending || !absent || guard.current) return;
    guard.current = true;
    setBusy(true);
    setAbsent(false);
    setError('');
    try {
      const result = await service.adjust(pending);
      sessionBoundary.assert(epoch);
      if (result.state === 'committed') {
        remember(null);
        setNotice(c('已确认原编号记账：{id}', { id: result.entry.identity }));
        wallet.refresh();
      } else setNotice(result.message);
    } catch (e) {
      if (epoch === sessionBoundary.epoch) {
        setError((e as Error).message);
        if (e instanceof ApiError && e.status >= 400 && e.status < 500) remember(null);
      }
    } finally {
      guard.current = false;
      if (epoch === sessionBoundary.epoch) setBusy(false);
    }
  };
  const pendingAction = pending
    ? pending.direction === 'add'
      ? c('新增{category}', {
          category: c(
            pending.category === 'recharge'
              ? '充值积分'
              : pending.category === 'gift'
                ? '赠送积分'
                : '历史未分类积分',
          ),
        })
      : c('扣减')
    : '';
  const pendingStatus = pending ? (
    <div className="op-pending" role="status">
      <p>{c('提交结果待确认 · {command}', { command: pending.command })}</p>
      <p>
        {c('用户 {account} · {action} {amount} 积分', {
          account: pending.account,
          action: pendingAction,
          amount: formatInteger(pending.amount),
        })}
      </p>
      <Button busy={busy} variant="outline" onClick={() => void reconcile()}>
        {c('按原编号核对')}
      </Button>
      {absent && !(pending.direction === 'add' && !pending.category) ? (
        <Button variant="outline" busy={busy} onClick={() => void retryOriginal()}>
          {c('原编号重试')}
        </Button>
      ) : absent ? (
        <>
          <p>
            {c(
              '该旧操作未指定积分分类，不能修改原内容后重发。已确认原编号不存在，可结束核对后重新选择类型。',
            )}
          </p>
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => {
              remember(null);
              setAbsent(false);
              setNotice(c('原操作未提交，可重新创建一笔指定类型的调整。'));
            }}
          >
            {c('结束本次核对')}
          </Button>
        </>
      ) : null}
      {!open ? (
        <Button variant="ghost" onClick={() => show({ identity: pending.account }, 'adjust')}>
          {c('查看待核对操作')}
        </Button>
      ) : null}
    </div>
  ) : null;
  return (
    <section className="op-page">
      <header className="op-page-heading">
        <div>
          <h1>{c('积分管理')}</h1>
          <p>{c('查看所有用户，按用户查询积分或记录调整。')}</p>
        </div>
      </header>
      <form
        className="op-filters"
        onSubmit={(event) => {
          event.preventDefault();
          setQuery(search.trim());
          setPage('');
          users.refresh();
        }}
      >
        <label className="op-search">
          <Search />
          <input
            aria-label={c('搜索用户邮箱')}
            placeholder={c('搜索用户邮箱')}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </label>
        <Button type="submit">{c('查询')}</Button>
      </form>
      <Feedback busy={users.busy} error={users.error} retry={users.refresh} />
      {users.value ? (
        <>
          <div className="op-table-scroll" tabIndex={0} aria-label={c('积分用户列表')}>
            <table className="op-table">
              <thead>
                <tr>
                  {['用户名', '邮箱', '账号角色', '状态', '积分操作'].map((label) => (
                    <th key={label}>{c(label)}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {users.value.items.map((account) => (
                  <tr key={account.identity}>
                    <td>{account.username}</td>
                    <td>{account.email}</td>
                    <td>
                      <StatusBadge tone={account.roleLabel === '运营人员' ? 'info' : 'neutral'}>
                        {account.roleLabel}
                      </StatusBadge>
                    </td>
                    <td>
                      <StatusBadge tone={account.enabled ? 'success' : 'danger'}>
                        {account.enabled ? c('正常') : c('已禁用')}
                      </StatusBadge>
                    </td>
                    <td>
                      <div className="op-row-actions op-credit-actions">
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={busy}
                          aria-label={c('余额与流水 {email}', { email: account.email })}
                          onClick={() => showUser(account, 'ledger')}
                        >
                          <List aria-hidden />
                          {c('余额与流水')}
                        </Button>
                        <Button
                          variant="default"
                          size="sm"
                          disabled={busy || !!pending}
                          aria-label={c('调整积分 {email}', { email: account.email })}
                          onClick={() => showUser(account, 'adjust')}
                        >
                          <SlidersHorizontal aria-hidden />
                          {c('调整积分')}
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Feedback busy={false} error="" empty={!users.value.items.length} retry={users.refresh} />
          <PageNavigation page={users.value} busy={users.busy} onPage={setPage} />
        </>
      ) : null}
      {!open ? (
        <>
          {pendingStatus}
          {error ? (
            <p className="op-inline-error" role="alert">
              {error}
            </p>
          ) : null}
          {notice ? (
            <p className="op-inline-note" role="status">
              {notice}
            </p>
          ) : null}
        </>
      ) : null}
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!busy) setOpen(value);
        }}
      >
        <DialogContent
          variant={mode === 'ledger' ? 'panel' : 'form'}
          className={
            mode === 'ledger' ? 'op-dialog op-wallet-dialog' : 'op-dialog op-credit-dialog'
          }
          closeDisabled={busy}
        >
          <DialogTitle>{mode === 'adjust' ? c('调整积分') : c('余额与流水')}</DialogTitle>
          <DialogDescription className="sr-only">
            {mode === 'adjust'
              ? c('为所选用户新增或扣减积分。')
              : c('查看所选用户的积分余额与流水。')}
          </DialogDescription>
          <Feedback
            busy={wallet.busy && !walletValue}
            error={wallet.error}
            retry={wallet.refresh}
          />
          {updates.error ? (
            <p className="op-inline-note" role="status">
              {updates.error}
            </p>
          ) : null}
          {walletValue ? (
            <>
              <dl className="op-balance" aria-label={c('积分余额')}>
                <div>
                  <dt>{c('可用积分')}</dt>
                  <dd>{formatInteger(walletValue.balance)}</dd>
                </div>
                <div>
                  <dt>{c('充值积分')}</dt>
                  <dd>{formatInteger(walletValue.rechargeBalance)}</dd>
                </div>
                <div>
                  <dt>{c('赠送及免费')}</dt>
                  <dd>{formatInteger(walletValue.giftBalance)}</dd>
                </div>
                {BigInt(walletValue.legacyBalance) > 0n ? (
                  <div>
                    <dt>{c('历史未分类')}</dt>
                    <dd>{formatInteger(walletValue.legacyBalance)}</dd>
                  </div>
                ) : null}
              </dl>
              {mode === 'ledger' ? (
                <>
                  <Ledger
                    page={walletValue.ledger}
                    onPage={setLedgerPage}
                    busy={wallet.busy}
                    pageSizeControl={
                      <CreditPageSizeChoice
                        value={ledgerPageSize}
                        disabled={wallet.busy}
                        onChange={(value) => {
                          setLedgerPageSize(value);
                          setLedgerPage('');
                        }}
                      />
                    }
                  />
                </>
              ) : (
                <form className="op-adjustment" onSubmit={adjust}>
                  <Field label={c('调整方向')}>
                    <Choice
                      label={c('调整方向')}
                      disabled={busy || !!pending}
                      value={direction}
                      options={[
                        { value: 'add', label: c('新增积分') },
                        { value: 'deduct', label: c('扣减积分') },
                      ]}
                      onChange={(value) =>
                        !busy && !pending && setDirection(value as typeof direction)
                      }
                    />
                  </Field>
                  {direction === 'add' ? (
                    <Field label={c('发放类型')}>
                      <Choice
                        label={c('发放类型')}
                        disabled={busy || !!pending}
                        value={category}
                        options={[
                          { value: 'gift', label: c('赠送及免费积分') },
                          { value: 'recharge', label: c('充值积分') },
                        ]}
                        onChange={(value) =>
                          !busy && !pending && setCategory(value as typeof category)
                        }
                      />
                    </Field>
                  ) : (
                    <p className="op-muted">{c('按赠送及免费积分、充值积分、历史积分的顺序扣减。')}</p>
                  )}
                  <Field label={c('积分（正整数）')}>
                    <input
                      inputMode="numeric"
                      pattern="[1-9][0-9]*"
                      value={amount}
                      onChange={(event) => setAmount(event.target.value)}
                      required
                      disabled={busy || !!pending}
                    />
                  </Field>
                  <Field label={c('原因')}>
                    <input
                      value={reason}
                      onChange={(event) => setReason(event.target.value)}
                      required
                      disabled={busy || !!pending}
                    />
                  </Field>
                  <footer>
                    <Button variant="outline" disabled={busy} onClick={() => setOpen(false)}>
                      {c('关闭')}
                    </Button>
                    <Button
                      type="submit"
                      variant={direction === 'deduct' ? 'destructive' : 'default'}
                      busy={busy}
                      disabled={!!pending}
                    >
                      {c('提交调整')}
                    </Button>
                  </footer>
                </form>
              )}
            </>
          ) : null}
          {open ? pendingStatus : null}
          {error ? (
            <p className="op-inline-error" role="alert">
              {error}
            </p>
          ) : null}
          {notice ? (
            <p className="op-inline-note" role="status">
              {notice}
            </p>
          ) : null}
        </DialogContent>
      </Dialog>
    </section>
  );
}
