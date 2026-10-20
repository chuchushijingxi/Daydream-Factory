import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Check, ChevronLeft, ChevronRight, FilePlus2, Send, Undo2 } from 'lucide-react';
import {
  notificationsApi,
  type NotificationsApi,
  type NotificationDraft,
  type NotificationMessage,
} from '../../api/notifications-client';
import { ApiError } from '../../api/http-error';
import { sessionBoundary } from '../../api/session-boundary';
import { useSession } from '../../state/SessionContext';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '../../components/ui/dialog';
import { SafeMarkdown } from '../../components/notifications/SafeMarkdown';
import { NotificationComposer } from '../../components/notifications/NotificationComposer';
import { useNotificationUpdates } from '../../components/notifications/useNotificationUpdates';
import { dateTime as notificationDate } from '../credits/credit-format';
import { useCatalogText } from '../../contexts/useCatalogText';
import { ModelSelect } from '../model-config/ModelConfigurationControls';
import { Field, StatusBadge, Feedback, useRead } from './controls';
import { NotificationRecipientPicker } from './NotificationRecipientPicker';
const blank: NotificationDraft = { title: '', body: '', audience: 'all', recipientUserId: '' };
const draftOf = (value: NotificationMessage): NotificationDraft => ({
  title: value.title,
  body: value.body,
  audience: value.audience,
  recipientUserId: value.recipientUserId,
});
const uncertain = (error: unknown) =>
  !(
    error instanceof ApiError &&
    error.status >= 400 &&
    error.status < 500 &&
    ![404, 409].includes(error.status)
  );
export function NotificationsPage({ client = notificationsApi }: { client?: NotificationsApi }) {
  const { user, epoch } = useSession();
  const { c } = useCatalogText();
  const [page, setPage] = useState(1),
    [selected, setSelected] = useState<NotificationMessage | null>(null),
    [draft, setDraft] = useState<NotificationDraft>(blank),
    [recipientLabel, setRecipientLabel] = useState('');
  const [recipients, setRecipients] = useState<Pick<
      NotificationMessage,
      'title' | 'recipientName' | 'recipientEmail'
    > | null>(null),
    recipientFocus = useRef<HTMLElement | null>(null);
  const [editing, setEditing] = useState(false),
    [confirm, setConfirm] = useState<'publish' | 'withdraw' | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [notice, setNotice] = useState(''),
    [needsRead, setNeedsRead] = useState(false);
  const command = useRef(''),
    active = useRef(true),
    sequence = useRef(0),
    guard = useRef(false),
    returnFocus = useRef<HTMLElement | null>(null);
  const list = useRead(() => client.operatorList(page), [client, page, epoch]);
  const syncError = useNotificationUpdates(client, list.refresh);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
      ++sequence.current;
    };
  }, []);
  useEffect(() => {
    setRecipients(null);
    setSelected(null);
    setEditing(false);
    setConfirm(null);
    setDraft(blank);
    setRecipientLabel('');
    setNeedsRead(false);
    command.current = '';
  }, [user?.identity, epoch]);
  const adopt = (value: NotificationMessage, open = true) => {
    setSelected(value);
    setDraft(draftOf(value));
    setRecipientLabel(value.recipientName);
    setEditing(open);
    setNeedsRead(false);
    list.refresh();
  };
  async function load(id?: string) {
    if (guard.current) return;
    guard.current = true;
    const ticket = ++sequence.current,
      generation = sessionBoundary.epoch;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const value = id
        ? await client.operatorDetail(id)
        : selected
          ? await client.operatorDetail(selected.id)
          : await client.command(command.current);
      sessionBoundary.assert(generation);
      if (active.current && ticket === sequence.current) {
        adopt(value);
      }
    } catch (error) {
      if (active.current && ticket === sequence.current && generation === sessionBoundary.epoch)
        setError((error as Error).message || c('通知无法读取。'));
    } finally {
      if (ticket === sequence.current) {
        guard.current = false;
        if (active.current && generation === sessionBoundary.epoch) setBusy(false);
      }
    }
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (guard.current || needsRead) return;
    if (draft.audience === 'user' && !draft.recipientUserId) {
      setError(c('请选择接收用户。'));
      return;
    }
    guard.current = true;
    const ticket = ++sequence.current,
      generation = sessionBoundary.epoch;
    setBusy(true);
    setError('');
    setNotice('');
    if (!command.current) command.current = crypto.randomUUID();
    try {
      const value = selected
        ? await client.update(selected.id, { ...draft, revision: selected.revision })
        : await client.create({ ...draft, commandId: command.current });
      sessionBoundary.assert(generation);
      if (active.current && ticket === sequence.current) {
        adopt(value);
        setNotice(c('草稿已保存，尚未向用户发布。'));
      }
    } catch (error) {
      if (active.current && generation === sessionBoundary.epoch && ticket === sequence.current) {
        setError((error as Error).message || c('保存结果未确认。'));
        setNeedsRead(uncertain(error));
      }
    } finally {
      if (ticket === sequence.current) {
        guard.current = false;
        if (active.current && generation === sessionBoundary.epoch) setBusy(false);
      }
    }
  }
  async function commit() {
    if (!selected || guard.current || needsRead) return;
    guard.current = true;
    const generation = sessionBoundary.epoch,
      ticket = ++sequence.current;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const value =
        confirm === 'withdraw'
          ? await client.withdraw(selected.id, selected.revision)
          : await client.publish(selected.id, selected.revision);
      sessionBoundary.assert(generation);
      if (active.current && ticket === sequence.current) {
        adopt(value, confirm !== 'withdraw');
        setNotice(
          value.status === 'withdrawn' ? c('通知已下线，接收用户不再可见。') : c('通知已发布。'),
        );
      }
    } catch (error) {
      if (active.current && generation === sessionBoundary.epoch && ticket === sequence.current) {
        setError((error as Error).message || c('提交结果未确认。'));
        setNeedsRead(uncertain(error));
      }
    } finally {
      if (ticket === sequence.current) {
        guard.current = false;
        if (active.current && generation === sessionBoundary.epoch) {
          setBusy(false);
          setConfirm(null);
        }
      }
    }
  }
  const patch = <K extends keyof NotificationDraft>(key: K, value: NotificationDraft[K]) => {
    setDraft((old) => ({ ...old, [key]: value }));
    setError('');
    setNotice('');
  };
  const changed = !!selected && JSON.stringify(draft) !== JSON.stringify(draftOf(selected)),
    readonly = !!selected && selected.status !== 'draft';
  async function uploadImage(file: File, signal: AbortSignal) {
    if (!selected || guard.current) throw new Error(c('请先保存草稿，再上传图片。'));
    guard.current = true;
    setBusy(true);
    const generation = sessionBoundary.epoch;
    try {
      return await client.uploadImage(selected.id, file, signal);
    } finally {
      guard.current = false;
      if (active.current && generation === sessionBoundary.epoch) setBusy(false);
    }
  }
  const close = (open: boolean) => {
    if (!guard.current) setEditing(open);
  };
  function create() {
    returnFocus.current = document.activeElement as HTMLElement;
    setSelected(null);
    setDraft(blank);
    setRecipientLabel('');
    setEditing(true);
    setConfirm(null);
    setError('');
    setNotice('');
    command.current = '';
  }
  const columns = [
    c('标题'),
    c('发布人'),
    c('接收范围'),
    c('创建时间'),
    c('发布时间'),
    c('状态'),
    c('操作'),
  ];
  const statusLabels = { draft: c('草稿'), published: c('已发布'), withdrawn: c('已下线') };
  return (
    <section className="op-page op-notifications-page">
      <header className="op-page-heading">
        <div>
          <h1>{c('站内信')}</h1>
          <p>{c('管理草稿与发布状态，通知发送到用户的工作台消息中心。')}</p>
        </div>
        <div className="op-row-actions">
          <Button disabled={busy || needsRead} onClick={create}>
            <FilePlus2 size={16} />
            {c('新建通知')}
          </Button>
        </div>
      </header>
      {syncError ? (
        <p className="op-inline-note" role="status">
          {syncError}
        </p>
      ) : null}
      {!editing && error ? (
        <p className="op-inline-error" role="alert">
          {error}
        </p>
      ) : null}
      {!editing && notice ? (
        <p className="op-inline-note" role="status">
          {notice}
        </p>
      ) : null}
      {!editing && needsRead ? (
        <div className="op-pending">
          <p>{c('上次通知提交结果待确认，请先读取原记录。')}</p>
          <Button variant="outline" onClick={() => setEditing(true)}>
            {c('核对待确认通知')}
          </Button>
        </div>
      ) : null}
      <Feedback
        busy={list.busy}
        error={list.error}
        empty={list.value?.total === 0}
        retry={list.refresh}
      />
      {list.value?.items.length ? (
        <div className="op-table-scroll" tabIndex={0} aria-label={c('站内信列表')}>
          <table className="op-table op-notification-table">
            <thead>
              <tr>
                {columns.map((label) => (
                  <th key={label}>{label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {list.value.items.map((value) => (
                <tr key={value.id}>
                  <td>
                    <strong>{value.title}</strong>
                  </td>
                  <td>{value.authorName}</td>
                  <td>
                    {value.audience === 'all' ? (
                      c('全部用户')
                    ) : (
                      <button
                        type="button"
                        className="notification-scope-link"
                        aria-label={c('查看接收用户 {title}', { title: value.title })}
                        onClick={(event) => {
                          recipientFocus.current = event.currentTarget;
                          setRecipients({
                            title: value.title,
                            recipientName: value.recipientName,
                            recipientEmail: value.recipientEmail,
                          });
                        }}
                      >
                        {c('指定用户')}
                      </button>
                    )}
                  </td>
                  <td>{notificationDate(value.createdAt)}</td>
                  <td>{value.publishedAt ? notificationDate(value.publishedAt) : '—'}</td>
                  <td>
                    <StatusBadge
                      tone={
                        value.status === 'published'
                          ? 'success'
                          : value.status === 'draft'
                            ? 'warning'
                            : 'neutral'
                      }
                    >
                      {statusLabels[value.status]}
                    </StatusBadge>
                    {value.withdrawnAt ? (
                      <small>{c('下线 {time}', { time: notificationDate(value.withdrawnAt) })}</small>
                    ) : null}
                  </td>
                  <td>
                    <div className="op-row-actions">
                      <Button
                        id={'notification-view-' + value.id}
                        variant="outline"
                        size="sm"
                        disabled={busy || needsRead}
                        aria-label={c('查看通知 {title}', { title: value.title })}
                        onClick={(event) => {
                          returnFocus.current = event.currentTarget;
                          void load(value.id);
                        }}
                      >
                        {value.status === 'draft' ? c('编辑') : c('查看')}
                      </Button>
                      {value.status === 'published' ? (
                        <Button
                          variant="destructive"
                          size="sm"
                          disabled={busy || needsRead}
                          aria-label={c('撤回通知 {title}', { title: value.title })}
                          onClick={(event) => {
                            returnFocus.current = event.currentTarget;
                            setSelected(value);
                            setDraft(draftOf(value));
                            setRecipientLabel(value.recipientName);
                            setError('');
                            setNotice('');
                            setConfirm('withdraw');
                          }}
                        >
                          <Undo2 size={14} aria-hidden />
                          {c('撤回下线')}
                        </Button>
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      {list.value ? (
        <nav className="op-pagination" aria-label={c('通知列表分页')}>
          <span>{c('共 {total} 条 · 第 {page} 页', { total: list.value.total, page })}</span>
          <Button
            variant="ghost"
            size="icon"
            aria-label={c('通知列表上一页')}
            disabled={busy || page <= 1}
            onClick={() => setPage((old) => old - 1)}
          >
            <ChevronLeft />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label={c('通知列表下一页')}
            disabled={busy || page * list.value.pageSize >= list.value.total}
            onClick={() => setPage((old) => old + 1)}
          >
            <ChevronRight />
          </Button>
        </nav>
      ) : null}
      <Dialog open={editing} onOpenChange={close}>
        <DialogContent
          variant="panel"
          className="notification-editor-dialog"
          {...(readonly ? { 'aria-describedby': undefined } : {})}
          closeDisabled={busy}
          onEscapeKeyDown={(event) => {
            event.stopPropagation();
            if (guard.current) event.preventDefault();
          }}
          onPointerDownOutside={(event) => {
            if (guard.current) event.preventDefault();
          }}
          onCloseAutoFocus={(event) => {
            if (returnFocus.current?.isConnected) {
              event.preventDefault();
              returnFocus.current.focus({ preventScroll: true });
            }
          }}
        >
          <DialogTitle>
            {readonly ? c('查看通知') : selected ? c('编辑草稿') : c('新建通知')}
          </DialogTitle>
          {!readonly ? (
            <DialogDescription>
              {c('保存草稿后再发布；发布前会再次确认标题与接收范围。')}
            </DialogDescription>
          ) : null}
          {readonly && selected ? (
            <article className="notification-published-body">
              <header>
                <h3>{selected.title}</h3>
                <p>
                  {selected.audience === 'all'
                    ? c('全部用户')
                    : c('指定用户 {name}', { name: selected.recipientName })}{' '}
                  · {selected.publishedAt && notificationDate(selected.publishedAt)} ·{' '}
                  {selected.authorName}
                </p>
                {selected.withdrawnAt ? (
                  <p>{c('已下线 · {time}', { time: notificationDate(selected.withdrawnAt) })}</p>
                ) : null}
              </header>
              <SafeMarkdown messageId={selected.id} operatorPreview>
                {selected.body}
              </SafeMarkdown>
            </article>
          ) : (
            <form onSubmit={save} className="op-notification-form" aria-busy={busy}>
              <div className="notification-editor-fields">
                <Field label={c('标题')}>
                  <input
                    value={draft.title}
                    maxLength={120}
                    required
                    disabled={busy || needsRead}
                    onChange={(event) => patch('title', event.target.value)}
                  />
                </Field>
                <Field label={c('接收范围')}>
                  <ModelSelect
                    label={c('通知接收范围')}
                    value={draft.audience}
                    options={[
                      { value: 'all', label: c('全部用户（含运营账号）') },
                      { value: 'user', label: c('指定用户') },
                    ]}
                    disabled={busy || needsRead}
                    onChange={(value) => {
                      setDraft((old) => ({
                        ...old,
                        audience: value as 'all' | 'user',
                        recipientUserId: '',
                      }));
                      setRecipientLabel('');
                    }}
                  />
                </Field>
                {draft.audience === 'user' ? (
                  <div className="op-field">
                    <span>{c('接收用户')}</span>
                    <NotificationRecipientPicker
                      value={draft.recipientUserId}
                      name={recipientLabel}
                      disabled={busy || needsRead}
                      onChange={(id, name) => {
                        patch('recipientUserId', id);
                        setRecipientLabel(name);
                      }}
                    />
                  </div>
                ) : null}
              </div>
              <NotificationComposer
                messageId={selected?.id}
                body={draft.body}
                disabled={busy || needsRead}
                onChange={(body) => patch('body', body)}
                onUpload={selected ? uploadImage : undefined}
              />
              {!selected ? (
                <p className="op-notification-help">
                  {c('先填写标题并保存草稿，即可上传图片。支持PNG/JPEG，最多5MB。')}
                </p>
              ) : null}
              <div className="op-notification-submit">
                <Button
                  type="submit"
                  variant="outline"
                  busy={busy}
                  disabled={
                    needsRead ||
                    !draft.title.trim() ||
                    (draft.audience === 'user' && !draft.recipientUserId)
                  }
                >
                  <Check size={15} />
                  {c('保存草稿')}
                </Button>
                <Button
                  type="button"
                  variant="default"
                  disabled={busy || !selected || changed || needsRead || !draft.body.trim()}
                  onClick={() => setConfirm('publish')}
                >
                  <Send size={15} />
                  {c('发布通知')}
                </Button>
              </div>
            </form>
          )}
          {notice ? (
            <p role="status" className="op-notification-notice">
              {notice}
            </p>
          ) : null}
          {error ? (
            <div role="alert" className="notification-error">
              {error}
            </div>
          ) : null}
          {needsRead ? (
            <div className="op-notification-reconcile">
              <p>{c('请先读取原通知核对结果，再继续操作。')}</p>
              <Button variant="outline" busy={busy} onClick={() => void load()}>
                {c('按原编号读取结果')}
              </Button>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
      <Dialog
        open={recipients !== null}
        onOpenChange={(open) => {
          if (!open) setRecipients(null);
        }}
      >
        <DialogContent
          variant="form"
          className="notification-recipients-dialog"
          onEscapeKeyDown={(event) => event.stopPropagation()}
          onCloseAutoFocus={(event) => {
            if (recipientFocus.current?.isConnected) {
              event.preventDefault();
              recipientFocus.current.focus({ preventScroll: true });
            }
          }}
        >
          <DialogTitle>{c('接收用户')}</DialogTitle>
          <DialogDescription>{recipients?.title}</DialogDescription>
          <div className="notification-recipient-scroll">
            <table className="notification-recipient-list" aria-label={c('接收用户列表')}>
              <thead>
                <tr>
                  <th scope="col">{c('用户名')}</th>
                  <th scope="col">{c('邮箱')}</th>
                </tr>
              </thead>
              <tbody>
                {recipients ? (
                  <tr>
                    <td>{recipients.recipientName}</td>
                    <td>{recipients.recipientEmail}</td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </DialogContent>
      </Dialog>
      <Dialog
        open={confirm !== null}
        onOpenChange={(open) => {
          if (!guard.current && !open) setConfirm(null);
        }}
      >
        <DialogContent
          variant="confirm"
          onCloseAutoFocus={(event) => {
            if (!editing) {
              event.preventDefault();
              const target =
                returnFocus.current?.isConnected && !returnFocus.current.matches(':disabled')
                  ? returnFocus.current
                  : document.getElementById('notification-view-' + selected?.id) ||
                    document.querySelector<HTMLButtonElement>('.op-pending button');
              target?.focus();
            }
          }}
          className="notification-confirm-dialog"
          closeDisabled={busy}
          onEscapeKeyDown={(event) => {
            event.stopPropagation();
            if (guard.current) event.preventDefault();
          }}
          onPointerDownOutside={(event) => {
            if (guard.current) event.preventDefault();
          }}
        >
          <DialogTitle>
            {confirm === 'withdraw' ? c('确认撤回下线') : c('确认发布通知')}
          </DialogTitle>
          <DialogDescription>
            {confirm === 'withdraw'
              ? c('下线后，所有接收用户将无法再查看这条通知及图片；发布记录仍保留。')
              : c('全部用户包含之后新注册的用户；指定用户仅对所选账号可见。')}
          </DialogDescription>
          <dl>
            <dt>{c('标题')}</dt>
            <dd>{selected?.title}</dd>
            <dt>{c('接收范围')}</dt>
            <dd>
              {selected?.audience === 'all'
                ? c('全部用户（含运营账号）')
                : selected?.recipientName}
            </dd>
          </dl>
          <footer>
            <Button variant="outline" disabled={busy} onClick={() => setConfirm(null)}>
              {confirm === 'withdraw' ? c('取消') : c('返回编辑')}
            </Button>
            <Button
              variant={confirm === 'withdraw' ? 'destructive' : 'default'}
              busy={busy}
              onClick={() => void commit()}
            >
              {confirm === 'withdraw' ? c('确认下线') : c('确认发布')}
            </Button>
          </footer>
        </DialogContent>
      </Dialog>
    </section>
  );
}
