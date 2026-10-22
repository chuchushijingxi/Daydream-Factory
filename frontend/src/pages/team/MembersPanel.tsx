import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useSession } from '../../state/SessionContext';
import { useSpace } from '../../state/SpaceContext';
import { teamClient } from '../../api/team-client';
import { useTeamRevision } from './useTeamUpdates';
import type { SpaceRole, TeamMember } from '../../api/spaces-types';
import { Button } from '../../components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from '../../components/ui/dialog';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Choice, Feedback, Field, useRead } from '../operations/controls';
import {
  ConfirmAction,
  dateOnly,
  InitialAvatar,
  MutationFeedback,
  numberInput,
  numberText,
  RoleBadge,
  MemberActionMenu,
  useTeamMutation,
} from './team-ui';
import { rangeLabel, type OverviewRange } from './OverviewPanel';
import { Search } from 'lucide-react';

function AllocationDialog({
  id,
  member,
  open,
  onPendingChange,
  onClose,
  onSaved,
}: {
  id: string;
  member: TeamMember;
  open: boolean;
  onPendingChange: (pending: boolean) => void;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { c } = useCatalogText(),
    mutation = useTeamMutation();
  const [kind, setKind] = useState<'allocate' | 'reclaim'>('allocate');
  const [amount, setAmount] = useState(''),
    [all, setAll] = useState(false),
    [command, setCommand] = useState(() => crypto.randomUUID()),
    [readbackBalance, setReadbackBalance] = useState<number | null>(null);
  const completed = useRef(false);
  useEffect(() => {
    // 成功已清除控制器后，排队的busy effect不能再次锁住父页。
    if (!completed.current) onPendingChange(mutation.busy || mutation.needsRead);
  }, [mutation.busy, mutation.needsRead, onPendingChange]);
  const locked = mutation.busy || mutation.needsRead;
  function changeAmount(value: string) {
    if (locked || value === amount) return;
    setAmount(value);
    setCommand(crypto.randomUUID());
    setReadbackBalance(null);
    mutation.reconciled();
  }
  function changeDirection(value: string) {
    if (locked || value === kind || (value !== 'allocate' && value !== 'reclaim')) return;
    setKind(value);
    // 全额只对收回有效，切回增加时不能把旧all意图带进allocate。
    if (value === 'allocate') setAll(false);
    setCommand(crypto.randomUUID());
    setReadbackBalance(null);
    mutation.reconciled();
  }
  async function submit(e: FormEvent) {
    e.preventDefault();
    await mutation.run(
      () =>
        teamClient.allocate(id, member.userId, kind, {
          commandId: command,
          ...(all ? { all: true } : { amount: numberInput(amount, 1) }),
        }),
      () => {
        completed.current = true;
        onPendingChange(false);
        onSaved();
        onClose();
      },
    );
  }
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!value && !mutation.busy) onClose();
      }}
    >
      <DialogContent className="team-dialog team-credit-adjustment" closeDisabled={mutation.busy}>
        <DialogTitle>{c('调整积分')}</DialogTitle>
        <DialogDescription>
          {c('成员：{name}。只操作名下未用积分，进行中任务已预扣的部分不可收回。', {
            name: member.name,
          })}
        </DialogDescription>
        <form className="team-form" onSubmit={submit}>
          <Field label={c('调整方向')}>
            <Choice
              label={c('调整方向')}
              value={kind}
              options={[
                { value: 'allocate', label: c('增加积分') },
                { value: 'reclaim', label: c('减少积分') },
              ]}
              onChange={changeDirection}
              disabled={locked}
            />
          </Field>
          <MutationFeedback mutation={mutation} />
          <Field label={c('积分数量')}>
            <input
              type="number"
              min="1"
              step="1"
              required={!all}
              disabled={all || mutation.busy || mutation.needsRead}
              value={amount}
              onChange={(e) => changeAmount(e.target.value)}
            />
          </Field>
          {kind === 'reclaim' && (
            <label className="team-check">
              <input
                type="checkbox"
                checked={all}
                disabled={mutation.busy || mutation.needsRead}
                onChange={(e) => {
                  if (locked || e.target.checked === all) return;
                  setAll(e.target.checked);
                  setCommand(crypto.randomUUID());
                  setReadbackBalance(null);
                  mutation.reconciled();
                }}
              />
              {c('收回全部未用积分')}
            </label>
          )}
          {mutation.needsRead && (
            <Button
              variant="outline"
              disabled={mutation.busy}
              onClick={() => {
                void mutation.readback(
                  () => teamClient.overview(id, 'contract'),
                  (value) =>
                    setReadbackBalance(
                      value.members.find((item) => item.userId === member.userId)?.holderBalance ??
                        null,
                    ),
                );
              }}
            >
              {c('读取最新状态')}
            </Button>
          )}
          {readbackBalance !== null && (
            <p>
              {c('名下可用积分')}：{numberText(readbackBalance)}
            </p>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={onClose} disabled={mutation.busy}>
              {c('取消')}
            </Button>
            <Button
              type="submit"
              busy={mutation.busy}
              disabled={mutation.needsRead || (!all && !amount)}
            >
              {c('确认调整')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
type RosterAction = 'remove' | 'leave' | 'role' | 'ownership';
export function MembersPanel({
  id,
  role,
  onChanged,
}: {
  id: string;
  role: SpaceRole;
  onChanged: () => void;
}) {
  const { c } = useCatalogText(),
    { user } = useSession(),
    space = useSpace(),
    mutation = useTeamMutation();
  const manager = role === 'owner' || role === 'admin';
  const read = useRead((signal) => teamClient.members(id, signal), [id, role]);
  const [range, setRange] = useState<OverviewRange>('30d'),
    [query, setQuery] = useState('');
  const statistics = useRead(
    (signal) => teamClient.overview(id, range, signal),
    [id, role, range],
    manager,
  );
  const balance = useRead(
    (signal) => teamClient.ownBalance(signal),
    [id, role, space.epoch],
    !manager && space.spaceId === id,
  );
  useTeamRevision(() => {
    read.reload();
    statistics.reload();
  });
  const [allocation, setAllocation] = useState<{
    member: TeamMember;
    open: boolean;
  } | null>(null);
  const [allocationPending, setAllocationPending] = useState(false);
  const [confirm, setConfirm] = useState<{ kind: RosterAction; member?: TeamMember } | null>(null);
  const refresh = () => {
    read.refresh();
    statistics.refresh();
    balance.refresh();
    onChanged();
  };
  const locked = mutation.busy || mutation.needsRead || allocationPending;
  // 未确认写只能通过显式回读解锁，回读成功后关闭确认窗再展示真实名单。
  const readMembers = () =>
    void mutation.readback(
      () => teamClient.members(id),
      () => {
        setConfirm(null);
        refresh();
      },
    );
  async function act() {
    if (!confirm) return;
    const action = confirm;
    await mutation.run(
      async () => {
        if (action.kind === 'leave') return teamClient.leave(id);
        if (!action.member) return;
        if (action.kind === 'remove') return teamClient.remove(id, action.member.userId);
        // 任免与所有权提议只对所有者开放；服务器同样核验。
        if (role !== 'owner') return;
        if (action.kind === 'ownership')
          return teamClient.beginOwnership(id, action.member.userId);
        return teamClient.role(
          id,
          action.member.userId,
          action.member.role === 'admin' ? 'member' : 'admin',
        );
      },
      async () => {
        setConfirm(null);
        if (action.kind === 'leave') {
          await space.refresh();
          if (user) await space.switchSpace(user.identity);
        } else refresh();
      },
    );
  }
  function rowActions(member: TeamMember) {
    const self = member.userId === user?.identity;
    if (!manager || self || member.role === 'owner') return [];
    const items: { label: string; run: () => void; destructive?: boolean }[] = [];
    if (role === 'owner') {
      items.push(
        {
          label: member.role === 'admin' ? c('设为成员') : c('设为管理员'),
          run: () => setConfirm({ kind: 'role', member }),
        },
        { label: c('转让所有权'), run: () => setConfirm({ kind: 'ownership', member }) },
      );
    }
    // 管理员须先降级才能移出。
    if (member.role === 'member')
      items.push({
        label: c('移出成员'),
        destructive: true,
        run: () => setConfirm({ kind: 'remove', member }),
      });
    return items;
  }
  const titles: Record<RosterAction, string> = {
    leave: c('退出团队'),
    remove: c('移出成员'),
    role: c('变更成员角色'),
    ownership: c('转让所有权'),
  };
  const descriptions: Record<RosterAction, string> = {
    leave: c('离开后将立即失去团队访问，名下未用积分退回未分配；内容保留，可由管理员转交。'),
    remove: c('离开后将立即失去团队访问，名下未用积分退回未分配；内容保留，可由管理员转交。'),
    role: c('角色变更会立即影响管理权限；升级为管理员时，名下未用积分退回团队未分配。'),
    ownership: c(
      '接收人须确认后才生效。新的提议会替换旧提议，旧确认立即失效，原所有者成为管理员。',
    ),
  };
  const keyword = query.trim().toLowerCase();
  const members = (read.value || []).filter(
    (member) =>
      !keyword ||
      member.name.toLowerCase().includes(keyword) ||
      member.email?.toLowerCase().includes(keyword),
  );
  return (
    <div className="team-stack team-members-panel">
      {!manager && (
        <div className="team-tiles" data-count="2">
          <div className="team-tile" data-primary="true">
            <span className="team-tile-label">{c('我的可用积分')}</span>
            <Feedback busy={balance.busy} error={balance.error} retry={balance.refresh} />
            {balance.value && (
              <strong className="team-tile-value team-own-balance">
                {numberText(balance.value.balance)}
              </strong>
            )}
            <span className="team-tile-hint">{c('由所有者或管理员分配')}</span>
          </div>
          <div className="team-tile">
            <span className="team-tile-label">{c('团队成员')}</span>
            <strong className="team-tile-value">
              {read.value ? numberText(read.value.length) : '—'}
            </strong>
            <span className="team-tile-hint">{c('人')}</span>
          </div>
        </div>
      )}
      <section className="team-card team-roster" aria-label={c('团队成员')}>
        <div className="team-card-head">
          {manager ? (
            <label className="team-search">
              <Search aria-hidden="true" />
              <input
                type="search"
                value={query}
                placeholder={c('搜索姓名或邮箱')}
                aria-label={c('搜索成员')}
                onChange={(event) => setQuery(event.target.value)}
              />
            </label>
          ) : (
            <h2>{c('团队成员')}</h2>
          )}
          {(manager || mutation.needsRead) && (
            <div className="team-record-toolbar">
              {manager && (
                <Choice
                  label={c('成员统计范围')}
                  value={range}
                  options={[
                    { value: '7d', label: c('近 7 天') },
                    { value: '30d', label: c('近 30 天') },
                    { value: 'contract', label: c('开通以来') },
                  ]}
                  onChange={(value) => setRange(value as OverviewRange)}
                />
              )}
              {mutation.needsRead && (
                <Button
                  variant="outline"
                  onClick={readMembers}
                  disabled={mutation.busy}
                >
                  {c('读取最新状态')}
                </Button>
              )}
            </div>
          )}
        </div>
        <MutationFeedback mutation={mutation} />
        {allocationPending && allocation && !allocation.open && (
          <Button variant="outline" onClick={() => setAllocation({ ...allocation, open: true })}>
            {c('核对未确认积分操作')}
          </Button>
        )}
        <Feedback busy={read.busy} error={read.error} retry={read.refresh} />
        {manager && (
          <Feedback busy={false} error={statistics.error} retry={statistics.refresh} />
        )}
        {read.value && (
          <div className="op-table-scroll" tabIndex={0} aria-label={c('团队成员')}>
            <table className="op-table team-roster-table">
              <thead>
                <tr>
                  <th>{c('成员')}</th>
                  <th>{c('团队角色')}</th>
                  {manager && (
                    <>
                      <th>{c('加入时间')}</th>
                      <th>{c('名下可用积分')}</th>
                      <th>{c('{range}消耗', { range: rangeLabel(range, c) })}</th>
                      <th>
                        <span className="sr-only">{c('操作')}</span>
                      </th>
                    </>
                  )}
                </tr>
              </thead>
              <tbody>
                {members.map((member) => {
                  const stats = statistics.value?.members.find(
                    (item) => item.userId === member.userId,
                  );
                  return (
                    <tr key={member.userId}>
                      <td>
                        <span className="team-person">
                          <InitialAvatar name={member.name} />
                          <span className="team-member-identity">
                            <span>
                              {member.name}
                              {member.userId === user?.identity && (
                                <span className="team-self">{c('（我）')}</span>
                              )}
                            </span>
                            {manager && member.email && <small>{member.email}</small>}
                          </span>
                        </span>
                      </td>
                      <td>
                        <RoleBadge role={member.role} />
                      </td>
                      {manager && (
                        <>
                          <td>
                            {member.joinedAt ? (
                              <time dateTime={member.joinedAt}>{dateOnly(member.joinedAt)}</time>
                            ) : (
                              '—'
                            )}
                          </td>
                          <td className="team-number">
                            {stats ? numberText(stats.holderBalance) : '—'}
                          </td>
                          <td className="team-number">{stats ? numberText(stats.credits) : '—'}</td>
                          <td className="team-row-actions">
                            {/* 管理者可调整成员积分；只有所有者能调整管理员的积分。 */}
                            {(member.role === 'member' ||
                              (member.role === 'admin' && role === 'owner')) && (
                              <Button
                                variant="outline"
                                size="sm"
                                disabled={locked}
                                aria-label={c('调整积分：{name}', { name: member.name })}
                                onClick={() => {
                                  if (!allocationPending) setAllocation({ member, open: true });
                                }}
                              >
                                {c('调整积分')}
                              </Button>
                            )}
                            <MemberActionMenu
                              name={member.name}
                              disabled={locked}
                              items={rowActions(member)}
                            />
                          </td>
                        </>
                      )}
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {!members.length && <p className="team-empty">{c('没有匹配的成员')}</p>}
          </div>
        )}
      </section>
      {role !== 'owner' && (
        <section className="team-card team-leave">
          <div>
            <h2>{c('退出团队')}</h2>
            <p className="op-muted">
              {c('退出后名下未用积分退回团队，已创建的内容保留在团队中。')}
            </p>
          </div>
          <Button
            variant="destructive"
            disabled={locked}
            onClick={() => setConfirm({ kind: 'leave' })}
          >
            {c('退出团队')}
          </Button>
        </section>
      )}
      {allocation && (
        <AllocationDialog
          key={allocation.member.userId}
          id={id}
          {...allocation}
          onPendingChange={setAllocationPending}
          onClose={() => setAllocation((value) => (value ? { ...value, open: false } : null))}
          onSaved={() => {
            setAllocationPending(false);
            setAllocation(null);
            refresh();
          }}
        />
      )}
      <ConfirmAction
        open={!!confirm}
        onOpenChange={(value) => {
          if (!value) setConfirm(null);
        }}
        title={confirm ? titles[confirm.kind] : ''}
        description={confirm ? descriptions[confirm.kind] : ''}
        busy={mutation.busy}
        confirmDisabled={mutation.needsRead}
        confirm={() => void act()}
      >
        {confirm?.member && <p className="team-confirm-target">{confirm.member.name}</p>}
        <MutationFeedback mutation={mutation} />
        {mutation.needsRead && (
          <Button variant="outline" disabled={mutation.busy} onClick={readMembers}>
            {c('读取最新状态')}
          </Button>
        )}
      </ConfirmAction>
    </div>
  );
}
