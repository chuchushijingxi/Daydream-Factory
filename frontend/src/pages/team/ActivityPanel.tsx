import { useState } from 'react';
import { teamClient } from '../../api/team-client';
import type { AuditEvent } from '../../api/spaces-types';
import { Button } from '../../components/ui/button';
import { useCatalogText, type CatalogText } from '../../contexts/useCatalogText';
import { Feedback, useRead } from '../operations/controls';
import { dateTime } from '../credits/credit-format';
import { InitialAvatar } from './team-ui';
export function actionText(action: string, c: CatalogText) {
  const labels: Record<string, string> = {
    team_opened: c('开通团队'),
    team_renamed: c('团队改名'),
    member_joined: c('成员加入'),
    member_left: c('成员离开'),
    member_removed: c('移出成员'),
    member_role_changed: c('变更成员角色'),
    ownership_proposed: c('提议转让所有权'),
    ownership_superseded: c('替换所有权提议'),
    ownership_transferred: c('所有权已转让'),
    content_transferred: c('内容转交'),
    resource_shared: c('共享资源'),
    resource_share_removed: c('取消共享'),
    join_request_approved: c('批准加入'),
    join_request_rejected: c('拒绝加入'),
    visibility_changed: c('变更可见性'),
    credits_allocate: c('分配积分'),
    credits_reclaim: c('收回积分'),
    invite_link_created: c('生成邀请链接'),
    invite_link_revoked: c('作废邀请'),
    email_invited: c('邮箱邀请'),
    invite_declined: c('拒绝邀请'),
    join_requested: c('申请加入'),
    team_renewed: c('续约团队'),
    team_enabled: c('启用团队'),
    team_disabled: c('停用团队'),
    member_limit_changed: c('修改人数上限'),
    grant_amount_changed: c('修改周期发放额'),
    model_discounts_changed: c('修改模型折扣'),
    owner_changed_by_operator: c('运营更换所有者'),
  };
  return labels[action] || action;
}
export function AuditTable({
  items,
  names = {},
}: {
  items: AuditEvent[];
  names?: Record<string, string>;
}) {
  const { c } = useCatalogText();
  return items.length ? (
    <div className="op-table-scroll" tabIndex={0} aria-label={c('团队操作记录')}>
      <table className="op-table">
        <thead>
          <tr>
            <th>{c('时间')}</th>
            <th>{c('操作')}</th>
            <th>{c('操作人')}</th>
            <th>{c('对象')}</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.id}>
              <td>{dateTime(item.createdAt)}</td>
              <td>{actionText(item.action, c)}</td>
              <td>
                <span className="team-person">
                  <InitialAvatar
                    name={item.actorKind === 'operator' ? c('运营人员') : names[item.actorId] || '?'}
                  />
                  <span className="team-member-identity">
                    <span className="team-identifier">{names[item.actorId] || item.actorId}</span>
                    {item.actorKind === 'operator' && <small>{c('运营人员')}</small>}
                  </span>
                </span>
              </td>
              <td className="team-identifier">{names[item.target] || item.target || '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  ) : (
    <p className="op-muted">{c('当前条件下暂无记录')}</p>
  );
}

export function ActivityPanel({ id }: { id: string }) {
  const { c } = useCatalogText();
  const [cursor, setCursor] = useState<number | undefined>();
  const history = useRead((signal) => teamClient.audit(id, cursor, signal), [id, cursor]);
  const members = useRead((signal) => teamClient.members(id, signal), [id]);
  const names = Object.fromEntries(
    (members.value || []).map((member) => [member.userId, member.name]),
  );
  return (
    <section className="team-card team-activity-panel" aria-label={c('团队操作记录')}>
      <div className="team-card-head">
        <div>
          <h2>{c('操作记录')}</h2>
          <p className="team-card-sub">{c('成员变动、积分分配、权限与设置的完整记录')}</p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            setCursor(undefined);
            history.refresh();
          }}
        >
          {c('最新记录')}
        </Button>
      </div>
      <Feedback busy={history.busy} error={history.error} retry={history.refresh} />
      {history.value && (
        <>
          <AuditTable items={history.value.items} names={names} />
          {history.value.nextCursor !== undefined && (
            <Button
              variant="outline"
              className="team-more-records"
              onClick={() => setCursor(history.value!.nextCursor)}
            >
              {c('更早记录')}
            </Button>
          )}
        </>
      )}
    </section>
  );
}
