import { useContext, useEffect, useRef, useState } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { Bell, CheckCheck, ChevronLeft, ChevronRight, Inbox, X } from 'lucide-react';
import { SessionOptionalContext, useSession } from '../../state/SessionContext';
import { sessionBoundary } from '../../api/session-boundary';
import { ApiError } from '../../api/http-error';
import {
  notificationsApi,
  type NotificationsApi,
  type NotificationDetail,
  type NotificationItem,
  type NotificationPage,
} from '../../api/notifications-client';
import { Button } from '../ui/button';
import { Dialog as ModalRoot, DialogContent as DetailDialogContent } from '../ui/dialog';
import { Spinner } from '../ui/spinner';
import { SafeMarkdown } from './SafeMarkdown';
import { useNotificationUpdates } from './useNotificationUpdates';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useNavigate } from 'react-router-dom';
import { useOptionalSpace } from '../../state/SpaceContext';
import { notificationTarget, type NotificationLink } from '../../api/notifications-client';
export const notificationDate = (at: string) =>
  new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: 'Asia/Shanghai',
  }).format(new Date(at));
function NotificationCenterContent({
  client = notificationsApi,
  compact = true,
}: {
  client?: NotificationsApi;
  compact?: boolean;
}) {
  const { c } = useCatalogText();
  const { user, epoch } = useSession();
  const [open, setOpen] = useState(false),
    [unread, setUnread] = useState(0),
    [badgeError, setBadgeError] = useState(false),
    [page, setPage] = useState(1);
  const [data, setData] = useState<NotificationPage<NotificationItem> | null>(null),
    [detailId, setDetailId] = useState<string | null>(null),
    [detail, setDetail] = useState<NotificationDetail | null>(null);
  const [listBusy, setListBusy] = useState(false),
    [detailBusy, setDetailBusy] = useState(false),
    [listError, setListError] = useState(''),
    [detailError, setDetailError] = useState(''),
    [notice, setNotice] = useState(''),
    [mutation, setMutation] = useState(false),
    [reload, setReload] = useState(0);
  const sequence = useRef(0),
    detailSequence = useRef(0),
    badgeSequence = useRef(0),
    active = useRef(true),
    trigger = useRef<HTMLButtonElement>(null),
    invalidateRef = useRef(() => {}),
    mutationGuard = useRef(false),
    detailReturn = useRef(''),
    listClose = useRef<HTMLButtonElement>(null);
  async function refreshUnread() {
    const ticket = ++badgeSequence.current,
      generation = sessionBoundary.epoch;
    try {
      const count = await client.unread();
      if (
        active.current &&
        generation === sessionBoundary.epoch &&
        ticket === badgeSequence.current
      ) {
        setUnread(count);
        setBadgeError(false);
      }
    } catch {
      if (
        active.current &&
        generation === sessionBoundary.epoch &&
        ticket === badgeSequence.current
      )
        setBadgeError(true);
    }
  }
  const invalidate = () => {
    ++sequence.current;
    ++detailSequence.current;
    // Drop old content before rereading: a withdrawal can invalidate even an
    // already-open detail or image. An event never supplies replacement content.
    if (open) {
      setData(null);
      setDetail(null);
      setListBusy(true);
      if (detailId) setDetailBusy(true);
    }
    setReload((value) => value + 1);
    void refreshUnread();
  };
  invalidateRef.current = invalidate;
  const syncError = useNotificationUpdates(client, invalidate);
  useEffect(() => {
    active.current = true;
    void refreshUnread();
    const focus = () => {
      if (document.visibilityState === 'visible') invalidateRef.current();
    };
    window.addEventListener('focus', focus);
    const timer = window.setInterval(focus, 60000);
    return () => {
      active.current = false;
      ++sequence.current;
      ++detailSequence.current;
      ++badgeSequence.current;
      window.removeEventListener('focus', focus);
      clearInterval(timer);
    };
  }, [client, user?.identity, epoch]);
  useEffect(() => {
    const ticket = ++sequence.current,
      generation = sessionBoundary.epoch;
    if (!open) return;
    setListBusy(true);
    setListError('');
    void client
      .list(page, 'all')
      .then((value) => {
        if (active.current && ticket === sequence.current && generation === sessionBoundary.epoch) {
          setData(value);
          ++badgeSequence.current;
          setUnread(value.unread);
          setBadgeError(false);
        }
      })
      .catch((error) => {
        if (active.current && ticket === sequence.current && generation === sessionBoundary.epoch)
          setListError(error.message || '通知暂时无法读取。');
      })
      .finally(() => {
        if (active.current && ticket === sequence.current && generation === sessionBoundary.epoch)
          setListBusy(false);
      });
  }, [open, page, reload, client, epoch]);
  useEffect(() => {
    const ticket = ++detailSequence.current,
      generation = sessionBoundary.epoch;
    if (!open || !detailId) return;
    setDetail(null);
    setDetailBusy(true);
    setDetailError('');
    const current = () =>
      active.current && ticket === detailSequence.current && generation === sessionBoundary.epoch;
    void (async () => {
      try {
        let value = await client.detail(detailId);
        sessionBoundary.assert(generation);
        if (!current()) return;
        setDetail(value);
        if (!value.readAt) {
          const receipt = await client.read(detailId);
          sessionBoundary.assert(generation);
          if (!current()) return;
          value = { ...value, readAt: receipt.readAt };
          setDetail(value);
          setData((old) =>
            old
              ? {
                  ...old,
                  items: old.items.map((item) =>
                    item.id === detailId ? { ...item, readAt: receipt.readAt } : item,
                  ),
                }
              : old,
          );
          await refreshUnread();
        }
      } catch (error) {
        if (current()) {
          if (error instanceof ApiError && error.status === 404) {
            setDetailId(null);
            setDetail(null);
            setNotice('该通知已下线或不再可见。');
            setReload((value) => value + 1);
            void refreshUnread();
          } else setDetailError((error as Error).message || '通知暂时无法读取。');
        }
      } finally {
        if (current()) setDetailBusy(false);
      }
    })();
  }, [open, detailId, reload, client, epoch]);
  async function readAll() {
    if (mutationGuard.current) return;
    mutationGuard.current = true;
    const generation = sessionBoundary.epoch;
    setMutation(true);
    setListError('');
    try {
      await client.readAll();
      sessionBoundary.assert(generation);
      if (active.current) {
        invalidate();
      }
    } catch (error) {
      if (active.current && generation === sessionBoundary.epoch)
        setListError((error as Error).message || '标记结果未确认，请重新读取。');
    } finally {
      mutationGuard.current = false;
      if (active.current && generation === sessionBoundary.epoch) setMutation(false);
    }
  }
  if (!user) return null;
  const busy = listBusy;
  const closeDetail = () => {
    ++detailSequence.current;
    setDetailId(null);
    setDetailError('');
  };
  return (
    <ModalRoot
      open={open}
      onOpenChange={(value) => {
        setOpen(value);
        if (!value) {
          ++sequence.current;
          ++detailSequence.current;
          setDetailId(null);
          setDetail(null);
        } else {
          setDetailId(null);
          setDetail(null);
          setData(null);
          setNotice('');
          setListError('');
          setDetailError('');
          setPage(1);
        }
      }}
    >
      <Dialog.Trigger asChild>
        <button
          ref={trigger}
          className="notification-trigger"
          data-compact={compact}
          title={badgeError ? c('通知暂不可用') : c('通知')}
          aria-label={
            badgeError
              ? c('通知暂不可用')
              : unread
                ? c('通知，{count}条未读', { count: unread })
                : c('通知')
          }
        >
          <Bell size={18} />
          {badgeError ? (
            <span className="notification-unavailable">!</span>
          ) : unread > 0 ? (
            <span className="notification-dot" />
          ) : null}
        </button>
      </Dialog.Trigger>
      <DetailDialogContent
        variant="drawer"
        hideClose
        overlayClassName="notification-drawer-mask"
        className="notification-drawer"
        aria-describedby={undefined}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          trigger.current?.focus({ preventScroll: true });
        }}
      >
        <div className="notification-drawer-inner">
          <header className="notification-heading">
            <Dialog.Title>{c('消息中心')}</Dialog.Title>
            <Dialog.Close asChild>
              <button
                ref={listClose}
                className="notification-icon-button"
                aria-label={c('关闭消息中心')}
              >
                <X size={18} />
              </button>
            </Dialog.Close>
          </header>
          {syncError ? (
            <p className="notification-sync-error" role="status">
              {syncError}
            </p>
          ) : null}
          {notice ? (
            <p className="notification-sync-error" role="status">
              {c(notice)}
            </p>
          ) : null}

          <div className="notification-actions">
            <span>
              {c('全部')}
              {data ? ` · ${c('{count} 条', { count: data.total })}` : ''}
            </span>
            <button disabled={mutation || unread === 0} onClick={() => void readAll()}>
              <CheckCheck size={14} />
              {c('全部已读')}
            </button>
          </div>
          <section className="notification-list" aria-label={c('全部通知')} aria-busy={busy}>
            {listError ? (
              <div className="notification-error" role="alert">
                {c(listError)}
                <Button variant="ghost" size="sm" onClick={() => setReload((value) => value + 1)}>
                  {c('重新读取')}
                </Button>
              </div>
            ) : listBusy && !data ? (
              <div className="notification-empty" role="status">
                <Spinner />
                {c('正在读取通知…')}
              </div>
            ) : data?.items.length ? (
              data.items.map((item) => (
                <button
                  key={item.id}
                  id={'notification-card-' + item.id}
                  className="notification-card"
                  onClick={() => {
                    detailReturn.current = item.id;
                    setNotice('');
                    setDetail(null);
                    setDetailBusy(true);
                    setDetailError('');
                    setDetailId(item.id);
                  }}
                  disabled={busy || mutation}
                >
                  <span className="notification-card-symbol" aria-hidden>
                    {c('剧')}
                    {!item.readAt ? <i /> : null}
                  </span>
                  <span className="notification-card-content">
                    <span className="notification-card-heading">
                      <strong>{item.title}</strong>
                      <time dateTime={item.publishedAt}>{notificationDate(item.publishedAt)}</time>
                    </span>
                    {item.summary ? (
                      <span className="notification-card-summary">{item.summary}</span>
                    ) : null}
                  </span>
                  {!item.readAt ? <span className="sr-only">{c('未读')}</span> : null}
                </button>
              ))
            ) : (
              <div className="notification-empty" role="status">
                <Inbox size={30} />
                <span>{c('暂无通知')}</span>
              </div>
            )}
          </section>
          {data && data.total > data.pageSize ? (
            <footer className="notification-pagination">
              <span>{c('第 {page} 页', { page })}</span>
              <button
                aria-label={c('通知上一页')}
                disabled={busy || page <= 1}
                onClick={() => setPage((value) => value - 1)}
              >
                <ChevronLeft size={16} />
              </button>
              <button
                aria-label={c('通知下一页')}
                disabled={busy || page * data.pageSize >= data.total}
                onClick={() => setPage((value) => value + 1)}
              >
                <ChevronRight size={16} />
              </button>
            </footer>
          ) : null}
        </div>
      </DetailDialogContent>
      <ModalRoot
        open={detailId !== null}
        onOpenChange={(value) => {
          if (!value) closeDetail();
        }}
      >
        <DetailDialogContent
          variant="settings"
          className="notification-detail-dialog"
          overlayClassName="notification-detail-mask"
          hideClose
          aria-describedby={undefined}
          onEscapeKeyDown={(event) => event.stopPropagation()}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            const card = document.getElementById('notification-card-' + detailReturn.current);
            (card instanceof HTMLButtonElement && !card.disabled ? card : listClose.current)?.focus(
              { preventScroll: true },
            );
          }}
        >
          <Dialog.Title className="jm-dialog-title">{detail?.title || c('通知详情')}</Dialog.Title>
          <Dialog.Close asChild>
            <button
              className="notification-icon-button notification-detail-close"
              aria-label={c('关闭通知详情')}
            >
              <X size={18} />
            </button>
          </Dialog.Close>
          <article className="notification-detail" aria-busy={detailBusy}>
            {detail ? (
              <>
                <time dateTime={detail.publishedAt}>{notificationDate(detail.publishedAt)}</time>
                <SafeMarkdown messageId={detail.id}>{detail.body}</SafeMarkdown>
                {detail.link && notificationTarget(detail.link) ? (
                  <NotificationTarget
                    link={detail.link}
                    onClose={() => {
                      closeDetail();
                      setOpen(false);
                    }}
                  />
                ) : null}
              </>
            ) : detailBusy ? (
              <div className="notification-empty" role="status">
                <Spinner />
                {c('正在核对通知…')}
              </div>
            ) : null}
            {detailError ? (
              <div className="notification-error" role="alert">
                {c(detailError)}
                <Button size="sm" variant="outline" onClick={() => setReload((value) => value + 1)}>
                  {c('重新读取')}
                </Button>
              </div>
            ) : null}
          </article>
        </DetailDialogContent>
      </ModalRoot>
    </ModalRoot>
  );
}
export function NotificationCenter({
  client = notificationsApi,
  compact = true,
}: {
  client?: NotificationsApi;
  compact?: boolean;
}) {
  const session = useContext(SessionOptionalContext);
  if (!session?.user) return null;
  return (
    <NotificationCenterContent
      key={session.user.identity + ':' + session.epoch}
      client={client}
      compact={compact}
    />
  );
}

function NotificationTarget({ link, onClose }: { link: NotificationLink; onClose: () => void }) {
  const { c } = useCatalogText(),
    space = useOptionalSpace(),
    navigate = useNavigate();
  const [busy, setBusy] = useState(false),
    guard = useRef(false);
  const open = async () => {
    const target = notificationTarget(link);
    if (!target || guard.current) return;
    guard.current = true;
    setBusy(true);
    try {
      if (
        target.switchSpace &&
        (!space || !(await space.switchSpace(link.spaceId, { keepRoute: true })))
      )
        return;
      onClose();
      navigate(target.path);
    } finally {
      guard.current = false;
      setBusy(false);
    }
  };
  return (
    <Button className="space-notification-action" busy={busy} onClick={() => void open()}>
      {c(
        link.type === 'invite'
          ? '查看邀请'
          : link.type === 'ownership_transfer'
            ? '确认所有权转让'
            : link.type === 'project'
              ? '打开项目'
              : '查看团队',
      )}
    </Button>
  );
}
