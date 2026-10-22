import { useEffect, useId, useState, type FormEvent } from 'react';
import { teamClient } from '../../api/team-client';
import { useTeamRevision } from './useTeamUpdates';
import { toast } from '../../components/ui/sonner';
import type { Invite, InviteLink } from '../../api/spaces-types';
import { Link2, Mail } from 'lucide-react';
import { Button } from '../../components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from '../../components/ui/dialog';
import { ViewTabs } from '../../components/ui/view-tabs';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Choice, Feedback, Field, StatusBadge, useRead } from '../operations/controls';
import { dateTime } from '../credits/credit-format';
import { dateOnly, InitialAvatar, MutationFeedback, useTeamMutation } from './team-ui';

export function InvitesPanel({
  id,
  blocked,
  onChanged,
  open,
  onOpenChange,
  view,
  onRequestCount,
}: {
  id: string;
  blocked: boolean;
  onChanged?: () => void;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  /** 由成员页的分区控制；未提供时组件自带“加入申请/邀请记录”切换。null 表示只保留弹窗。 */
  view?: 'requests' | 'invites' | null;
  onRequestCount?: (count: number) => void;
}) {
  const { c } = useCatalogText(),
    mutation = useTeamMutation();
  const read = useRead((signal) => teamClient.invites(id, signal), [id]);
  const requests = useRead((signal) => teamClient.requests(id, signal), [id]);
  useTeamRevision(() => {
    read.reload();
    requests.reload();
  });
  // 弹窗关闭或历史页签切换时，父级继续保留草稿和未确认命令。
  const [localOpen, setLocalOpen] = useState(false),
    [method, setMethod] = useState('email'),
    [email, setEmail] = useState(''),
    [link, setLink] = useState<InviteLink | null>(null),
    [copyError, setCopyError] = useState(''),
    [history, setHistory] = useState<'requests' | 'invites'>('requests');
  const panelId = useId();
  const shown = view === undefined ? history : view;
  useEffect(() => {
    if (requests.value) onRequestCount?.(requests.value.length);
  }, [requests.value]);
  const visible = open ?? localOpen;
  const feedback = { ...mutation, notice: '' };
  function reportOpen(value: boolean) {
    if (open === undefined) setLocalOpen(value);
    onOpenChange?.(value);
  }
  // 申请已被其他管理员或其他窗口处理时不是未知结果：静默重读列表并提示，不锁定面板。
  const decide = (request: string, approve: boolean) =>
    void mutation.run(
      async () => {
        try {
          await teamClient.decideRequest(id, request, approve);
          return true;
        } catch (error) {
          if ((error as { code?: string }).code === 'TEAM_REQUEST_CONFLICT') return false;
          throw error;
        }
      },
      (decided) => {
        if (!decided) toast.info(c('该申请已被处理，列表已更新。'));
        requests.reload();
        if (approve) {
          read.reload();
          onChanged?.();
        }
      },
    );
  const refresh = () =>
    void mutation.readback(
      () => Promise.all([teamClient.invites(id), teamClient.requests(id)]),
      () => {
        read.refresh();
        requests.refresh();
      },
    );
  function inviteStatus(item: Invite) {
    if (item.revokedAt) return c('已作废');
    if (item.status === 'pending')
      return new Date(item.expiresAt).getTime() <= Date.now() ? c('已过期') : c('待处理');
    if (item.status === 'accepted') return c('已接受');
    if (item.status === 'declined') return c('已拒绝');
    if (item.status === 'expired') return c('已过期');
    return item.status;
  }
  function inviteTone(item: Invite) {
    return !item.revokedAt &&
      item.status === 'pending' &&
      new Date(item.expiresAt).getTime() > Date.now()
      ? ('info' as const)
      : ('neutral' as const);
  }
  const url = link
    ? window.location.origin +
      window.location.pathname +
      '#/invite/' +
      encodeURIComponent(link.token)
    : '';
  async function invite(event: FormEvent) {
    event.preventDefault();
    if (blocked || mutation.busy || mutation.needsRead) return;
    if (method === 'link') {
      await mutation.run(
        () => teamClient.createLink(id),
        (value) => {
          setLink(value);
          setCopyError('');
          read.refresh();
        },
      );
      return;
    }
    if (!email.trim()) return;
    await mutation.run(
      () => teamClient.inviteEmail(id, email.trim()),
      () => {
        setEmail('');
        read.refresh();
        onChanged?.();
        reportOpen(false);
      },
    );
  }
  return (
    <div className="team-stack team-invitations team-invite-history">
      {(open === undefined || (mutation.needsRead && !visible)) && (
        <div className="team-invite-history-actions">
          {open === undefined && (
            <Button onClick={() => reportOpen(true)} disabled={mutation.busy}>
              {c('邀请成员')}
            </Button>
          )}
          {mutation.needsRead && !visible && (
            <Button variant="outline" disabled={mutation.busy} onClick={() => reportOpen(true)}>
              {c('核对未确认操作')}
            </Button>
          )}
        </div>
      )}
      {!visible && <MutationFeedback mutation={feedback} />}
      {view === undefined && (
        <ViewTabs
          label={c('成员历史')}
          prefix={panelId + '-'}
          panelId={panelId + '-content'}
          value={history}
          variant="pill"
          onChange={(value) => setHistory(value as typeof history)}
          items={[
            { value: 'requests', label: c('加入申请') },
            { value: 'invites', label: c('邀请记录') },
          ]}
        />
      )}
      <div
        id={panelId + '-content'}
        role={view === undefined ? 'tabpanel' : undefined}
        aria-labelledby={view === undefined ? panelId + '-' + history : undefined}
        hidden={shown === null}
      >
        <section
          className="team-card team-request-list"
          aria-label={c('加入申请')}
          hidden={shown !== 'requests'}
        >
          <Feedback busy={requests.busy} error={requests.error} retry={refresh} />
          {requests.value && !requests.value.length && (
            <p className="team-empty">{c('暂无待处理的加入申请。团队 ID 可在“设置”中复制给申请人。')}</p>
          )}
          {requests.value?.map((item) => (
            <article className="team-invite-row" key={item.id}>
              <InitialAvatar name={item.userName} />
              <div>
                <strong>{item.userName}</strong>
                <p className="team-preserve">{item.message || c('未填写申请说明')}</p>
                <small>{dateTime(item.createdAt)}</small>
              </div>
              <div className="op-row-actions">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={mutation.busy || mutation.needsRead}
                  onClick={() => decide(item.id, false)}
                >
                  {c('拒绝')}
                </Button>
                <Button
                  size="sm"
                  disabled={blocked || mutation.busy || mutation.needsRead}
                  onClick={() => decide(item.id, true)}
                >
                  {c('批准加入')}
                </Button>
              </div>
            </article>
          ))}
        </section>
        <section
          className="team-card team-invitation-records"
          aria-label={c('邀请记录')}
          hidden={shown !== 'invites'}
        >
          <Feedback busy={read.busy} error={read.error} retry={refresh} />
          {read.value && !read.value.length && (
            <p className="team-empty">{c('还没有发出邀请。')}</p>
          )}
          {read.value?.map((item) => (
            <div className="team-invite-row" key={item.id}>
              <span className="team-invite-kind" aria-hidden="true">
                {item.kind === 'link' ? <Link2 /> : <Mail />}
              </span>
              <div>
                <strong>{item.kind === 'link' ? c('邀请链接') : c('邮箱邀请')}</strong>
                <p>{item.inviteeUserId || c('持有链接的已注册用户')}</p>
                <small>
                  {c('{created} 发出 · 有效期至 {time}', {
                    created: dateOnly(item.createdAt),
                    time: dateTime(item.expiresAt),
                  })}
                </small>
              </div>
              <StatusBadge tone={inviteTone(item)}>{inviteStatus(item)}</StatusBadge>
              {item.kind === 'link' && !item.revokedAt && item.status === 'pending' && (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={mutation.busy || mutation.needsRead}
                  onClick={() =>
                    void mutation.run(
                      () => teamClient.revokeLink(id, item.id),
                      () => {
                        if (link?.id === item.id) setLink(null);
                        read.refresh();
                      },
                    )
                  }
                >
                  {c('作废链接')}
                </Button>
              )}
            </div>
          ))}
        </section>
      </div>
      <Dialog
        open={visible}
        onOpenChange={(value) => {
          if (!mutation.busy) reportOpen(value);
        }}
      >
        <DialogContent className="team-dialog team-invite-dialog" closeDisabled={mutation.busy}>
          <DialogTitle>{c('邀请成员')}</DialogTitle>
          <DialogDescription className="sr-only">{c('邀请已注册用户加入团队。')}</DialogDescription>
          <form className="team-form" onSubmit={invite}>
            <Field label={c('邀请方式')}>
              <Choice
                label={c('邀请方式')}
                value={method}
                options={[
                  { value: 'email', label: c('邮箱邀请') },
                  { value: 'link', label: c('邀请链接') },
                ]}
                disabled={mutation.busy || mutation.needsRead}
                onChange={(value) => {
                  if (!mutation.busy && !mutation.needsRead) setMethod(value);
                }}
              />
            </Field>
            {method === 'email' ? (
              <Field label={c('已注册用户邮箱')} hint={c('仅可邀请已注册用户，邀请 7 天内有效。')}>
                <input
                  type="email"
                  required
                  value={email}
                  disabled={mutation.busy || mutation.needsRead}
                  onChange={(event) => setEmail(event.target.value)}
                />
              </Field>
            ) : (
              <>
                <p className="op-muted">
                  {c('链接 7 天内有效；明文只在本次生成后显示，请及时复制。')}
                </p>
                {link && (
                  <div className="team-invite-dialog-receipt">
                    <Field label={c('本次邀请链接')}>
                      <div className="team-invite-copy-row">
                        <input
                          aria-label={c('邀请链接')}
                          readOnly
                          value={url}
                          onFocus={(event) => event.target.select()}
                        />
                        <Button
                          variant="outline"
                          onClick={() => {
                            setCopyError('');
                            if (!navigator.clipboard) {
                              setCopyError(c('复制失败，请手动选择链接复制。'));
                              return;
                            }
                            void navigator.clipboard
                              .writeText(url)
                              .catch(() => setCopyError(c('复制失败，请手动选择链接复制。')));
                          }}
                        >
                          {c('复制链接')}
                        </Button>
                      </div>
                    </Field>
                    <small>{c('有效期至 {time}', { time: dateTime(link.expiresAt) })}</small>
                    {copyError && (
                      <p role="alert" className="op-inline-error">
                        {copyError}
                      </p>
                    )}
                  </div>
                )}
              </>
            )}
            {blocked && (
              <p className="team-callout">
                {c('团队当前不能接纳新成员，请核对到期时间和人数上限。')}
              </p>
            )}
            <MutationFeedback mutation={feedback} />
            {mutation.needsRead && (
              <Button variant="outline" disabled={mutation.busy} onClick={refresh}>
                {c('读取最新状态')}
              </Button>
            )}
            <DialogFooter>
              <Button variant="outline" disabled={mutation.busy} onClick={() => reportOpen(false)}>
                {c('取消')}
              </Button>
              <Button
                type="submit"
                busy={mutation.busy}
                disabled={blocked || mutation.needsRead || (method === 'email' && !email.trim())}
              >
                {method === 'email' ? c('发送邀请') : c('生成邀请链接')}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
