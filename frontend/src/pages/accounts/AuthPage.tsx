import { authenticationMessage } from '../../api/errors';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { ArrowLeft, Moon, ShieldCheck, Sun } from 'lucide-react';
import { Button } from '../../components/ui/button';
import { useWorkspace } from '../../state/WorkspaceContext';
import { useSession } from '../../state/SessionContext';
import { useRead, Field } from '../operations/controls';
import type { AuthForm } from '../../api/administration';
import { sessionBoundary } from '../../api/session-boundary';
import { safeReturnTo } from '../../api/space-selection';
import { PlatformLogo } from '../../components/workspace/PlatformLogo';
import { usePlatform } from '../../state/PlatformContext';
import { AuthScene } from './AuthScene';
import { useCatalogText } from '../../contexts/useCatalogText';
const emptyForm: AuthForm = {
  username: '',
  email: '',
  password: '',
  emailCode: '',
  registrationGrant: '',
};
export function AuthPage() {
  return <AuthenticationForm operations={false} />;
}
export function OperationsAuthPage() {
  return <AuthenticationForm operations />;
}
function AuthenticationForm({ operations }: { operations: boolean }) {
  const { c } = useCatalogText();
  const { settings: brand } = usePlatform();
  const { pathname, search } = useLocation(),
    session = useSession(),
    { theme, toggleTheme } = useWorkspace();
  const mode =
    !operations && pathname === '/register'
      ? 'register'
      : pathname === (operations ? '/operations/forgot-password' : '/forgot-password')
        ? 'reset'
        : 'login';
  const returnTo = operations ? '' : safeReturnTo(new URLSearchParams(search).get('returnTo'));
  const carry = (path: string) =>
    path + (returnTo ? '?returnTo=' + encodeURIComponent(returnTo) : '');
  const loginPath = carry(operations ? '/operations-login' : '/login');
  const [method, setMethod] = useState<'password' | 'email'>('password'),
    [form, setForm] = useState(emptyForm),
    [error, setError] = useState(''),
    [notice, setNotice] = useState(''),
    [busy, setBusy] = useState(false),
    [sending, setSending] = useState(false);
  const active = useRef(true),
    guard = useRef(false),
    sendGuard = useRef(false);
  const options = useRead(() => session.service.authOptions(), [session.service]);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  useEffect(() => {
    setForm(emptyForm);
    setError('');
    setNotice('');
  }, [mode, operations, method]);
  const patch = (key: keyof AuthForm, value: string) => {
    setForm((old) => ({ ...old, [key]: value }));
    setError('');
    setNotice('');
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (guard.current) return;
    guard.current = true;
    setBusy(true);
    setError('');
    setNotice('');
    const epoch = sessionBoundary.epoch;
    try {
      if (mode === 'reset') {
        await session.service.resetPassword(form);
        sessionBoundary.assert(epoch);
        if (active.current) {
          setForm(emptyForm);
          setNotice('密码已重置，原会话已失效。请重新登录。');
        }
      } else if (mode === 'register') {
        await session.service.register({
          ...form,
          registrationGrant: options.value?.registrationGrantRequired ? form.registrationGrant : '',
        });
        sessionBoundary.assert(epoch);
        if (active.current) {
          setForm(emptyForm);
          setNotice('注册成功，请登录。');
        }
      } else await session.authenticate(() => session.service.login(method, form, operations));
    } catch (e) {
      if (active.current && epoch === sessionBoundary.epoch) setError(authenticationMessage(e));
    } finally {
      guard.current = false;
      if (active.current) setBusy(false);
    }
  };
  const send = async () => {
    if (sendGuard.current || !form.email) return;
    sendGuard.current = true;
    setSending(true);
    setError('');
    const epoch = sessionBoundary.epoch;
    try {
      await session.service.sendCode(
        form.email,
        mode === 'register' ? 'register' : mode === 'reset' ? 'reset' : 'login',
      );
      sessionBoundary.assert(epoch);
      if (active.current) {
        setNotice('验证码已发送，请查看邮箱。');
      }
    } catch (e) {
      if (active.current && epoch === sessionBoundary.epoch) setError(authenticationMessage(e));
    } finally {
      sendGuard.current = false;
      if (active.current) setSending(false);
    }
  };
  const needsCode = mode !== 'login' || method === 'email';
  const title =
    mode === 'register'
      ? c('创建账号')
      : mode === 'reset'
        ? c('找回密码')
        : operations
          ? c('登录运营后台')
          : brand
            ? c('登录 {name}', { name: brand.name })
            : c('登录');
  return (
    <main className={'account-page' + (operations ? '' : ' account-creative')}>
      <header className="account-top">
        {operations ? (
          <Link className="account-brand" to={loginPath}>
            <PlatformLogo />
            <span className="account-brand-name" title={brand?.name}>
              {brand?.name}
            </span>
            <span className="account-brand-purpose">{c('运营后台')}</span>
          </Link>
        ) : null}
        <Button
          variant="ghost"
          size="icon"
          aria-label={c('切换外观')}
          title={theme === 'dark' ? c('切换浅色') : c('切换深色')}
          onClick={toggleTheme}
        >
          {theme === 'dark' ? <Sun /> : <Moon />}
        </Button>
      </header>
      <div className={operations ? undefined : 'account-layout'}>
        {!operations ? <AuthScene /> : null}
        <div className={operations ? undefined : 'account-form-panel'}>
          <section className="account-card" aria-labelledby="auth-title">
            {operations ? (
              <div className="account-symbol">
                <ShieldCheck />
              </div>
            ) : null}
            <h1 id="auth-title">{title}</h1>
            {mode !== 'login' || operations ? (
              <p>
                {mode === 'register'
                  ? c('用邮箱创建你的创作空间')
                  : mode === 'reset'
                    ? c('通过邮箱验证码设置新密码')
                    : c('使用具有运营权限的账号登录')}
              </p>
            ) : null}
            {mode === 'login' ? (
              <div className="op-tabs" role="group" aria-label={c('登录方式')}>
                <button
                  type="button"
                  aria-pressed={method === 'password'}
                  disabled={busy || sending}
                  onClick={() => setMethod('password')}
                >
                  {c('密码登录')}
                </button>
                <button
                  type="button"
                  aria-pressed={method === 'email'}
                  disabled={busy || sending}
                  onClick={() => setMethod('email')}
                >
                  {c('验证码登录')}
                </button>
              </div>
            ) : null}
            <form onSubmit={submit} className="account-form">
              {mode === 'register' ? (
                <Field label={c('用户名')} hint={c('1–40个字符，用于展示；登录时仍使用邮箱。')}>
                  <input
                    autoComplete="nickname"
                    maxLength={80}
                    required
                    value={form.username}
                    disabled={busy || sending}
                    onChange={(e) => patch('username', e.target.value)}
                  />
                </Field>
              ) : null}
              <Field label={needsCode ? c('邮箱') : c('账号或邮箱')}>
                <input
                  type={needsCode ? 'email' : 'text'}
                  autoComplete="username"
                  placeholder={needsCode ? c('输入邮箱地址') : c('输入账号或邮箱')}
                  required
                  value={form.email}
                  disabled={busy || sending}
                  onChange={(e) => patch('email', e.target.value)}
                />
              </Field>
              {needsCode ? (
                <Field label={c('邮箱验证码')}>
                  <div className="account-code">
                    <input
                      aria-label={c('邮箱验证码')}
                      inputMode="numeric"
                      autoComplete="one-time-code"
                      required
                      value={form.emailCode}
                      disabled={busy}
                      onChange={(e) => patch('emailCode', e.target.value)}
                    />
                    <Button
                      variant="outline"
                      disabled={busy || !form.email}
                      busy={sending}
                      onClick={() => void send()}
                    >
                      {c('发送验证码')}
                    </Button>
                  </div>
                </Field>
              ) : null}
              {mode !== 'login' || method === 'password' ? (
                <Field label={mode === 'reset' ? c('新密码') : c('密码')}>
                  <input
                    type="password"
                    autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
                    placeholder={
                      operations ? undefined : mode === 'reset' ? c('设置新密码') : c('输入密码')
                    }
                    required
                    value={form.password}
                    disabled={busy}
                    onChange={(e) => patch('password', e.target.value)}
                  />
                </Field>
              ) : null}
              {mode === 'register' && options.value?.registrationGrantRequired ? (
                <Field label={c('注册授权码')} hint={c('注册资格凭证，与邮箱验证码不同。')}>
                  <input
                    autoComplete="off"
                    required
                    value={form.registrationGrant}
                    disabled={busy}
                    onChange={(e) => patch('registrationGrant', e.target.value)}
                  />
                </Field>
              ) : null}
              {error || options.error || session.message ? (
                <p className="op-inline-error" role="alert">
                  {c(error || options.error || session.message)}
                </p>
              ) : null}
              {notice ? (
                <p className="op-inline-note" role="status">
                  {c(notice)}
                </p>
              ) : null}
              <Button
                className="account-submit"
                type="submit"
                busy={busy}
                disabled={
                  session.state === 'checking' ||
                  options.busy ||
                  !options.value ||
                  (mode === 'login' &&
                    !(method === 'password'
                      ? options.value.passwordLogin
                      : options.value.emailLogin))
                }
              >
                {mode === 'register' ? c('注册') : mode === 'reset' ? c('重置密码') : c('登录')}
              </Button>
            </form>
            <div className="account-links">
              {mode === 'login' ? (
                <>
                  {!operations ? <Link to={carry('/register')}>{c('注册账号')}</Link> : null}
                  <Link to={carry(operations ? '/operations/forgot-password' : '/forgot-password')}>
                    {c('忘记密码')}
                  </Link>
                </>
              ) : (
                <Link to={loginPath}>
                  <ArrowLeft size={14} />
                  {c('返回登录')}
                </Link>
              )}
            </div>
            {options.error ? (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  options.refresh();
                  void session.refresh();
                }}
              >
                {c('重新检查服务')}
              </Button>
            ) : null}
          </section>
        </div>
      </div>
    </main>
  );
}
