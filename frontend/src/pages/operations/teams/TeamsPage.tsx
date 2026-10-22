import { useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { teamClient } from '../../../api/team-client';
import { useSession } from '../../../state/SessionContext';
import { useCatalogText } from '../../../contexts/useCatalogText';
import { Button } from '../../../components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '../../../components/ui/dialog';
import { Feedback, Field, PageNavigation, StatusBadge, useRead } from '../controls';
import { dateTime, shanghaiInput } from '../../credits/credit-format';
import {
  MutationFeedback,
  numberInput,
  numberText,
  useTeamMutation,
} from '../../team/team-ui';
import { TeamContractFields, discountsComplete, type ContractDraft } from './TeamContractFields';

function OpenTeam({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (id: string) => void;
}) {
  const { c } = useCatalogText(),
    { service } = useSession(),
    mutation = useTeamMutation();
  const [name, setName] = useState(''),
    [search, setSearch] = useState(''),
    [query, setQuery] = useState(''),
    [page, setPage] = useState(''),
    [owner, setOwner] = useState<{ id: string; name: string } | null>(null);
  const [draft, setDraft] = useState<ContractDraft>({
    memberLimit: '',
    expiresAt: '',
    grantAmount: '',
    modelDiscounts: [],
  });
  const users = useRead(() => service.users(query, page), [service, query, page]);
  const models = useRead((signal) => teamClient.models(signal), []);
  const patch = (value: Partial<ContractDraft>) => setDraft((old) => ({ ...old, ...value }));
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!owner) return;
    await mutation.run(
      () =>
        teamClient.openTeam({
          name: name.trim(),
          ownerId: owner.id,
          memberLimit: numberInput(draft.memberLimit, 1),
          expiresAt: shanghaiInput(draft.expiresAt),
          grantAmount: numberInput(draft.grantAmount),
          modelDiscounts: draft.modelDiscounts,
        }),
      (value) => onCreated(value.id),
    );
  }
  return (
    <Dialog
      open
      onOpenChange={(value) => {
        if (!value && !mutation.busy) onClose();
      }}
    >
      <DialogContent variant="settings" closeDisabled={mutation.busy}>
        <DialogTitle>{c('开通团队')}</DialogTitle>
        <DialogDescription>
          {c('按线下合同开通。开通即按合同发放首期积分，所有者必须为已注册用户。')}
        </DialogDescription>
        <div className="team-form">
          <Field label={c('搜索已注册用户')}>
            <input value={search} onChange={(e) => setSearch(e.target.value)} />
          </Field>
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              setQuery(search.trim());
              setPage('');
              users.refresh();
            }}
          >
            {c('搜索用户')}
          </Button>
          <Feedback busy={users.busy} error={users.error} retry={users.refresh} />
          {users.value && (
            <>
              <div className="team-user-picker" role="group" aria-label={c('选择团队所有者')}>
                {users.value.items.map((user) => (
                  <button
                    type="button"
                    className="team-user-option"
                    aria-pressed={owner?.id === user.identity}
                    key={user.identity}
                    disabled={!user.enabled || mutation.busy || mutation.needsRead}
                    onClick={() => setOwner({ id: user.identity, name: user.username })}
                  >
                    <span>{user.username}</span>
                    <small>{user.email}</small>
                  </button>
                ))}
              </div>
              <PageNavigation page={users.value} busy={users.busy} onPage={setPage} />
            </>
          )}
          {owner && <p>{c('已选择所有者：{name}', { name: owner.name })}</p>}
        </div>
        <form className="team-form" onSubmit={submit}>
          <Field label={c('团队名称')}>
            <input
              required
              maxLength={40}
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={mutation.busy || mutation.needsRead}
            />
          </Field>
          <Feedback busy={models.busy} error={models.error} retry={models.refresh} />
          <TeamContractFields
            value={draft}
            patch={patch}
            models={models.value || []}
            disabled={mutation.busy || mutation.needsRead}
          />
          <MutationFeedback mutation={mutation} />
          {mutation.needsRead && (
            <p className="team-callout">
              {c('请关闭弹窗后在团队列表核对开通结果；不要再次开通相同团队。')}
            </p>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={onClose} disabled={mutation.busy}>
              {c('取消')}
            </Button>
            <Button
              type="submit"
              busy={mutation.busy}
              disabled={
                !owner || !name.trim() || mutation.needsRead || models.busy || !!models.error || !discountsComplete(draft)
              }
            >
              {c('确认开通')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
export function TeamsPage() {
  const { c } = useCatalogText(),
    navigate = useNavigate();
  const [search, setSearch] = useState(''),
    [query, setQuery] = useState(''),
    [page, setPage] = useState(1),
    [opening, setOpening] = useState(false);
  const read = useRead((signal) => teamClient.teams(query, page, signal), [query, page]);
  return (
    <section className="op-page team-page">
      <header className="op-page-heading">
        <div>
          <h1>{c('团队管理')}</h1>
          <p>{c('管理团队合同、所有者与启用状态。')}</p>
        </div>
        <Button onClick={() => setOpening(true)}>{c('开通团队')}</Button>
      </header>
      <form
        className="op-filters"
        onSubmit={(e) => {
          e.preventDefault();
          setQuery(search.trim());
          setPage(1);
          read.refresh();
        }}
      >
        <Field label={c('搜索团队名称、ID 或所有者邮箱')}>
          <input value={search} onChange={(e) => setSearch(e.target.value)} />
        </Field>
        <Button type="submit">{c('查询')}</Button>
      </form>
      <Feedback
        busy={read.busy}
        error={read.error}
        empty={!read.value?.items.length}
        retry={read.refresh}
      />
      {read.value && (
        <>
          <div className="op-table-scroll" tabIndex={0} aria-label={c('团队列表')}>
            <table className="op-table">
              <thead>
                <tr>
                  {[
                    c('团队'),
                    c('所有者'),
                    c('人数 / 状态'),
                    c('到期时间'),
                    c('每 30 天发放积分'),
                    c('团队总余额'),
                    c('操作'),
                  ].map((label) => (
                    <th key={label}>{label}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {read.value.items.map((team) => (
                  <tr key={team.id}>
                    <td>{team.name}</td>
                    <td>
                      {team.ownerName}
                      <small>{team.ownerEmail}</small>
                    </td>
                    {/* TeamState 只渲染到期/停用等提醒；列表单元格直接给出人数与状态事实。 */}
                    <td className="op-team-state">
                      <span>
                        {c('{count} / {limit} 人', {
                          count: team.memberCount,
                          limit: team.memberLimit,
                        })}
                      </span>
                      <StatusBadge
                        tone={
                          team.status === 'active'
                            ? 'success'
                            : team.status === 'expired'
                              ? 'warning'
                              : 'danger'
                        }
                      >
                        {team.status === 'active'
                          ? c('正常')
                          : team.status === 'expired'
                            ? c('已到期')
                            : c('已停用')}
                      </StatusBadge>
                    </td>
                    <td>{dateTime(team.expiresAt)}</td>
                    <td>{numberText(team.grantAmount)}</td>
                    <td>{numberText(team.balance)}</td>
                    <td>
                      <Button asChild variant="outline" size="sm">
                        <Link to={'/operations/teams/' + encodeURIComponent(team.id)}>
                          {c('合同详情')}
                        </Link>
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="op-pagination">
            <span>
              {c('第 {page} 页 · 共 {count} 个团队', {
                page: read.value.page,
                count: read.value.total,
              })}
            </span>
            <Button
              variant="outline"
              disabled={read.busy || page <= 1}
              onClick={() => setPage((old) => old - 1)}
            >
              {c('上一页')}
            </Button>
            <Button
              variant="outline"
              disabled={read.busy || read.value.page * read.value.pageSize >= read.value.total}
              onClick={() => setPage((old) => old + 1)}
            >
              {c('下一页')}
            </Button>
          </div>
        </>
      )}
      {opening && (
        <OpenTeam
          onClose={() => {
            setOpening(false);
            read.refresh();
          }}
          onCreated={(id) => {
            setOpening(false);
            navigate('/operations/teams/' + encodeURIComponent(id));
          }}
        />
      )}
    </section>
  );
}
