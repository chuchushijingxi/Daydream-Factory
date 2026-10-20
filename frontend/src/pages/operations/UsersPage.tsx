import { useRef, useState, type FormEvent } from 'react';
import { Search } from 'lucide-react';
import { useSession } from '../../state/SessionContext';
import { sessionBoundary } from '../../api/session-boundary';
import { Button } from '../../components/ui/button';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Feedback, PageNavigation, useRead, StatusBadge } from './controls';
import { dateTime } from '../credits/credit-format';
export function UsersPage() {
  const { service, user } = useSession(),
    { c } = useCatalogText(),
    [search, setSearch] = useState(''),
    [query, setQuery] = useState(''),
    [page, setPage] = useState(''),
    [busy, setBusy] = useState(''),
    [error, setError] = useState('');
  const guard = useRef(false);
  const read = useRead(() => service.users(query, page), [service, query, page, user?.username]);
  const toggle = async (identity: string, enabled: boolean) => {
    if (guard.current) return;
    guard.current = true;
    setBusy(identity);
    setError('');
    const epoch = sessionBoundary.epoch;
    try {
      await service.setUserEnabled(identity, enabled);
      sessionBoundary.assert(epoch);
      read.refresh();
    } catch (e) {
      if (epoch === sessionBoundary.epoch) setError((e as Error).message);
    } finally {
      guard.current = false;
      if (epoch === sessionBoundary.epoch) setBusy('');
    }
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setQuery(search.trim());
    setPage('');
    read.refresh();
  };
  // 用“账号角色”区分资产模块的“角色”，避免中文同词在英文里都落到 Character。
  const userColumns = [
    c('用户名'),
    c('邮箱'),
    c('账号角色'),
    c('状态'),
    c('注册时间'),
    c('操作'),
  ];
  return (
    <section className="op-page">
      <header className="op-page-heading">
        <div>
          <h1>{c('用户管理')}</h1>
          <p>{c('管理账号状态，查看必要的账号资料。')}</p>
        </div>
      </header>
      <form className="op-filters" onSubmit={submit}>
        <label className="op-search">
          <Search />
          <input
            aria-label={c('搜索邮箱或用户 ID')}
            placeholder={c('搜索邮箱或用户 ID')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </label>
        <Button type="submit">{c('查询')}</Button>
      </form>
      <Feedback busy={read.busy} error={read.error} retry={read.refresh} />
      {error ? (
        <p className="op-inline-error" role="alert">
          {c('{message} 请回读账号状态确认。', { message: error })}
        </p>
      ) : null}
      {read.value ? (
        <>
          <div className="op-table-scroll" tabIndex={0} aria-label={c('用户列表')}>
            <table className="op-table">
              <thead>
                <tr>
                  {userColumns.map((x) => (
                    <th key={x}>{x}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {read.value.items.map((u) => (
                  <tr key={u.identity}>
                    <td>{u.username}</td>
                    <td>{u.email}</td>
                    <td>
                      <StatusBadge tone={c(u.roleLabel) === c('运营人员') ? 'info' : 'neutral'}>
                        {c(u.roleLabel)}
                      </StatusBadge>
                    </td>
                    <td>
                      <StatusBadge tone={u.enabled ? 'success' : 'danger'}>
                        {u.enabled ? c('正常') : c('已禁用')}
                      </StatusBadge>
                    </td>
                    <td>{dateTime(u.createdAt)}</td>
                    <td>
                      <div className="op-row-actions">
                        <Button
                          size="sm"
                          variant={u.enabled ? 'destructive' : 'success'}
                          disabled={!!busy}
                          busy={busy === u.identity}
                          onClick={() => void toggle(u.identity, !u.enabled)}
                        >
                          {u.enabled ? c('禁用') : c('启用')}
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Feedback busy={false} error="" empty={!read.value.items.length} retry={read.refresh} />
          <PageNavigation page={read.value} busy={read.busy} onPage={setPage} />
        </>
      ) : null}
    </section>
  );
}
