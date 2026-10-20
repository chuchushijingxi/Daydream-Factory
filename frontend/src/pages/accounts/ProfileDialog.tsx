import { useEffect, useId, useRef, useState, type FormEvent, type RefObject } from 'react';
import { Camera, Check, LockKeyhole, UserRound } from 'lucide-react';
import { useSession } from '../../state/SessionContext';
import { ApiError } from '../../api/http-error';
import { sessionBoundary } from '../../api/session-boundary';
import { authenticationMessage, writeResultUnknown } from '../../api/errors';
import type { SessionView } from '../../api/administration';
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '../../components/ui/dialog';
import { Button } from '../../components/ui/button';
import { AccountAvatar } from './AccountAvatar';
import { useCatalogText } from '../../contexts/useCatalogText';

export function ProfileDialog({
  open,
  onOpenChange,
  returnFocus,
}: {
  open: boolean;
  onOpenChange(open: boolean): void;
  returnFocus?: RefObject<HTMLButtonElement>;
}) {
  const session = useSession(),
    [busy, setBusy] = useState(false),
    guard = useRef(false);
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!guard.current) onOpenChange(next);
      }}
    >
      <DialogContent
        variant="form"
        className="account-profile-dialog"
        closeDisabled={busy}
        onEscapeKeyDown={(event) => {
          event.stopPropagation();
          if (guard.current) event.preventDefault();
        }}
        onPointerDownOutside={(event) => {
          if (guard.current) event.preventDefault();
        }}
        onCloseAutoFocus={(event) => {
          if (returnFocus?.current?.isConnected) {
            event.preventDefault();
            returnFocus.current.focus({ preventScroll: true });
          }
        }}
      >
        {session.user ? (
          <ProfileForm
            close={() => onOpenChange(false)}
            onBusy={(value) => {
              guard.current = value;
              setBusy(value);
            }}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
function ProfileForm({ close, onBusy }: { close(): void; onBusy(value: boolean): void }) {
  const { c } = useCatalogText();
  const session = useSession(),
    user = session.user!,
    id = useId();
  const [tab, setTab] = useState<'profile' | 'security'>('profile'),
    [username, setUsername] = useState(user.username);
  const [current, setCurrent] = useState(''),
    [password, setPassword] = useState(''),
    [confirmation, setConfirmation] = useState('');
  const [busy, setBusy] = useState(''),
    [error, setError] = useState(''),
    [success, setSuccess] = useState(''),
    [needsRead, setNeedsRead] = useState(true),
    [invalid, setInvalid] = useState('');
  const fileInput = useRef<HTMLInputElement>(null),
    nameInput = useRef<HTMLInputElement>(null),
    currentInput = useRef<HTMLInputElement>(null),
    newInput = useRef<HTMLInputElement>(null),
    confirmationInput = useRef<HTMLInputElement>(null),
    active = useRef(true),
    guard = useRef(false);
  async function run(kind: string, action: () => Promise<SessionView | void>, message: string) {
    if (guard.current) return;
    guard.current = true;
    onBusy(true);
    setBusy(kind);
    setError('');
    setSuccess('');
    setInvalid('');
    const epoch = sessionBoundary.epoch;
    try {
      const saved = await action();
      sessionBoundary.assert(epoch);
      if (active.current) {
        if (saved && (kind === 'profile' || kind === 'read')) setUsername(saved.username);
        setNeedsRead(false);
        setSuccess(message);
      }
    } catch (e) {
      if (active.current && epoch === sessionBoundary.epoch) {
        setError(authenticationMessage(e));
        const code = e instanceof ApiError ? e.code : '';
        setInvalid(code);
        if (kind === 'read' || writeResultUnknown(e)) setNeedsRead(true);
        const field =
          code === 'INVALID_USERNAME'
            ? nameInput
            : code === 'CURRENT_PASSWORD_INVALID'
              ? currentInput
              : code === 'INVALID_PASSWORD'
                ? newInput
                : null;
        if (field)
          requestAnimationFrame(() => {
            if (active.current && epoch === sessionBoundary.epoch)
              field.current?.focus({ preventScroll: true });
          });
      }
    } finally {
      guard.current = false;
      if (active.current) {
        setBusy('');
        onBusy(false);
      }
    }
  }
  const readback = (message: string) =>
    run(
      'read',
      () =>
        session.updateProfile(async () => {
          const current = await session.service.current();
          if (!current) throw new ApiError(401, 'AUTH_REQUIRED', '请重新登录。');
          return current;
        }),
      message,
    );
  useEffect(() => {
    active.current = true;
    void readback('');
    return () => {
      active.current = false;
    };
  }, []);
  function save(event: FormEvent) {
    event.preventDefault();
    if (!needsRead)
      void run(
        'profile',
        () => session.updateProfile(() => session.service.saveProfile(username)),
        '个人资料已保存',
      );
  }
  function changePassword(event: FormEvent) {
    event.preventDefault();
    if (needsRead) return;
    setError('');
    setSuccess('');
    setInvalid('');
    if (password !== confirmation) {
      setInvalid('CONFIRMATION');
      setError('两次输入的新密码不一致');
      confirmationInput.current?.focus({ preventScroll: true });
      return;
    }
    const bytes = new TextEncoder().encode(password).length;
    if (bytes < 12 || bytes > 72) {
      setInvalid('INVALID_PASSWORD');
      setError('新密码须为12至72字节');
      newInput.current?.focus({ preventScroll: true });
      return;
    }
    void run('password', () => session.changePassword(current, password), '');
  }
  const disabled = !!busy,
    writeDisabled = disabled || needsRead,
    feedbackId = id + '-feedback';
  const feedback = (
    <>
      <ProfileFeedback error={error} success={success} id={feedbackId} />
      {needsRead && !disabled ? (
        <div className="account-profile-readback">
          <p>{c('尚未确认最新资料，请先读取后核对。')}</p>
          <Button
            variant="outline"
            size="sm"
            onClick={() => void readback('已读取最新资料，请核对结果。')}
          >
            {c('读取最新资料')}
          </Button>
        </div>
      ) : null}
    </>
  );
  function selectTab(next: typeof tab) {
    setTab(next);
    setError('');
    setSuccess('');
    setInvalid('');
  }
  return (
    <>
      <DialogTitle>{c('账号设置')}</DialogTitle>
      <DialogDescription>{c('管理个人资料与登录密码，登录邮箱保持独立。')}</DialogDescription>
      <div
        className="account-profile-tabs"
        role="tablist"
        aria-label={c('账号设置分类')}
        onKeyDown={(event) => {
          if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key) || disabled) return;
          event.preventDefault();
          const next =
            event.key === 'Home'
              ? 'profile'
              : event.key === 'End'
                ? 'security'
                : tab === 'profile'
                  ? 'security'
                  : 'profile';
          selectTab(next);
          document.getElementById(id + '-' + next)?.focus({ preventScroll: true });
        }}
      >
        <button
          type="button"
          role="tab"
          id={id + '-profile'}
          tabIndex={tab === 'profile' ? 0 : -1}
          aria-selected={tab === 'profile'}
          aria-controls={id + '-profile-panel'}
          disabled={disabled}
          onClick={() => selectTab('profile')}
        >
          <UserRound aria-hidden />
          {c('个人资料')}
        </button>
        <button
          type="button"
          role="tab"
          id={id + '-security'}
          tabIndex={tab === 'security' ? 0 : -1}
          aria-selected={tab === 'security'}
          aria-controls={id + '-security-panel'}
          disabled={disabled}
          onClick={() => selectTab('security')}
        >
          <LockKeyhole aria-hidden />
          {c('账号安全')}
        </button>
      </div>
      {tab === 'profile' ? (
        <form
          id={id + '-profile-panel'}
          role="tabpanel"
          aria-labelledby={id + '-profile'}
          onSubmit={save}
          aria-busy={disabled}
        >
          <div className="account-profile-photo">
            <AccountAvatar user={user} />
            <div>
              <Button
                variant="secondary"
                size="sm"
                disabled={writeDisabled}
                busy={busy === 'avatar'}
                onClick={() => fileInput.current?.click()}
              >
                <Camera aria-hidden />
                {c('更换头像')}
              </Button>
              <p>{c('JPG、PNG、WebP，最多5MB')}</p>
              {user.avatarUrl ? (
                <button
                  type="button"
                  className="account-text-action"
                  disabled={writeDisabled}
                  onClick={() =>
                    void run(
                      'avatar',
                      () => session.updateProfile(() => session.service.removeAvatar()),
                      '头像已移除',
                    )
                  }
                >
                  {c('移除头像')}
                </button>
              ) : null}
            </div>
          </div>
          <input
            ref={fileInput}
            type="file"
            accept="image/jpeg,image/png,image/webp"
            aria-label={c('选择头像')}
            hidden
            disabled={writeDisabled}
            onChange={(event) => {
              const file = event.target.files?.[0];
              event.target.value = '';
              if (!file || writeDisabled) return;
              if (
                !file.size ||
                file.size > 5 * 1024 * 1024 ||
                !['image/jpeg', 'image/png', 'image/webp'].includes(file.type)
              ) {
                setSuccess('');
                setError('请选择5MB以内的JPG、PNG或WebP图片');
                return;
              }
              void run(
                'avatar',
                () => session.updateProfile(() => session.service.uploadAvatar(file)),
                '头像已更新',
              );
            }}
          />
          <label className="account-field">
            {c('用户名')}
            <input
              ref={nameInput}
              value={username}
              onChange={(event) => {
                setUsername(event.target.value);
                setSuccess('');
                if (invalid === 'INVALID_USERNAME') {
                  setInvalid('');
                  setError('');
                }
              }}
              autoComplete="nickname"
              maxLength={80}
              required
              disabled={disabled}
              aria-invalid={invalid === 'INVALID_USERNAME' || undefined}
              aria-describedby={invalid === 'INVALID_USERNAME' ? feedbackId : undefined}
            />
            <small>{c('1–40个字符，将显示在工作台中')}</small>
          </label>
          <div className="account-email">
            <span>{c('登录邮箱')}</span>
            <strong>{user.email}</strong>
          </div>
          {feedback}
          <footer>
            <Button variant="outline" disabled={disabled} onClick={close}>
              {c('关闭')}
            </Button>
            <Button
              type="submit"
              busy={busy === 'profile'}
              disabled={writeDisabled || username.trim() === user.username}
            >
              {c('保存资料')}
            </Button>
          </footer>
        </form>
      ) : (
        <form
          id={id + '-security-panel'}
          role="tabpanel"
          aria-labelledby={id + '-security'}
          onSubmit={changePassword}
          aria-busy={disabled}
        >
          <p className="account-password-note">{c('修改成功后需要重新登录。')}</p>
          <label className="account-field">
            {c('原密码')}
            <input
              ref={currentInput}
              type="password"
              value={current}
              onChange={(event) => setCurrent(event.target.value)}
              autoComplete="current-password"
              required
              disabled={disabled}
              aria-invalid={invalid === 'CURRENT_PASSWORD_INVALID' || undefined}
            />
          </label>
          <label className="account-field">
            {c('新密码')}
            <input
              ref={newInput}
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="new-password"
              required
              disabled={disabled}
              aria-invalid={invalid === 'INVALID_PASSWORD' || undefined}
            />
            <small>{c('12–72字节')}</small>
          </label>
          <label className="account-field">
            {c('确认新密码')}
            <input
              ref={confirmationInput}
              type="password"
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
              autoComplete="new-password"
              required
              disabled={disabled}
              aria-invalid={invalid === 'CONFIRMATION' || undefined}
            />
          </label>
          {feedback}
          <footer>
            <Button variant="outline" disabled={disabled} onClick={close}>
              {c('关闭')}
            </Button>
            <Button type="submit" busy={busy === 'password'} disabled={writeDisabled}>
              {c('修改密码')}
            </Button>
          </footer>
        </form>
      )}
    </>
  );
}
function ProfileFeedback({ error, success, id }: { error: string; success: string; id: string }) {
  const { c } = useCatalogText();
  return (
    <>
      {error ? (
        <p id={id} className="account-feedback account-error" role="alert">
          {c(error)}
        </p>
      ) : null}
      {success ? (
        <p id={id} className="account-feedback" role="status">
          <Check aria-hidden />
          {c(success)}
        </p>
      ) : null}
    </>
  );
}
