import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useSession } from '../../state/SessionContext';
import { sessionBoundary } from '../../api/session-boundary';
import { ApiError } from '../../api/http-error';
import { writeResultUnknown } from '../../api/errors';
import type { SmtpSettingsDTO, SmtpUpdateDTO } from '../../api/administration-types';
import { Button } from '../../components/ui/button';
import { useCatalogText, type CatalogText } from '../../contexts/useCatalogText';
import { ModelSelect, ModelSwitch } from '../model-config/ModelConfigurationControls';
import { Field, Feedback } from './controls';

type Draft = Pick<SmtpSettingsDTO, 'enabled' | 'address' | 'username' | 'from' | 'tls'>;
const initial: Draft = { enabled: false, address: '', username: '', from: '', tls: 'starttls' };
const draftOf = ({ enabled, address, username, from, tls }: SmtpSettingsDTO): Draft => ({
  enabled,
  address,
  username,
  from,
  tls,
});
function message(error: unknown, c: CatalogText) {
  const value = error as { code?: string; message?: string };
  return (
    (
      {
        MAIL_CONFIGURATION_INVALID: c('邮件配置无效，请核对连接、发送邮箱、密码和加密方式。'),
        MAIL_CONFIGURATION_CONFLICT: c('配置已被更新，请读取最新配置后再修改。'),
        MAIL_CONFIGURATION_UNAVAILABLE: c('邮件配置暂时无法读取，请稍后重试。'),
        MAIL_CONFIG_SAVE_UNKNOWN: c('保存结果未确认，请先读取最新配置核对。'),
      } as Record<string, string>
    )[value.code || ''] ||
    value.message ||
    c('邮件配置请求失败。')
  );
}
export function MailSettingsPage() {
  const { service } = useSession();
  const { c } = useCatalogText();
  const [saved, setSaved] = useState<SmtpSettingsDTO | null>(null),
    [draft, setDraft] = useState(initial),
    [password, setPassword] = useState(''),
    [clearPassword, setClearPassword] = useState(false);
  const [busy, setBusy] = useState<'load' | 'save' | ''>('load'),
    [error, setError] = useState(''),
    [notice, setNotice] = useState(''),
    [needsRead, setNeedsRead] = useState(true);
  const active = useRef(false),
    guard = useRef(false),
    sequence = useRef(0);
  const adopt = (value: SmtpSettingsDTO) => {
    setSaved(value);
    setDraft(draftOf(value));
    setPassword('');
    setClearPassword(false);
    setNeedsRead(false);
  };
  async function load(first = false) {
    if (guard.current) return;
    guard.current = true;
    const ticket = ++sequence.current,
      epoch = sessionBoundary.epoch;
    setBusy('load');
    setError('');
    setNotice('');
    try {
      const value = await service.smtp();
      sessionBoundary.assert(epoch);
      if (active.current && ticket === sequence.current) {
        adopt(value);
        setNotice(first ? '' : c('已读取最新配置，请核对后继续。'));
      }
    } catch (e) {
      if (active.current && epoch === sessionBoundary.epoch && ticket === sequence.current) {
        setError(message(e, c));
        setNeedsRead(true);
      }
    } finally {
      if (ticket === sequence.current) {
        guard.current = false;
        if (active.current && epoch === sessionBoundary.epoch) setBusy('');
      }
    }
  }
  useEffect(() => {
    guard.current = false;
    active.current = true;
    void load(true);
    return () => {
      active.current = false;
      ++sequence.current;
    };
  }, [service]);
  const patch = <K extends keyof Draft>(key: K, value: Draft[K]) => {
    setDraft((old) => ({ ...old, [key]: value }));
    setError('');
    setNotice('');
  };
  async function save(event: FormEvent) {
    event.preventDefault();
    if (guard.current || needsRead || !saved) return;
    guard.current = true;
    const epoch = sessionBoundary.epoch,
      ticket = ++sequence.current;
    setBusy('save');
    setError('');
    setNotice('');
    const input: SmtpUpdateDTO = {
      ...draft,
      revision: saved.revision,
      ...(password ? { password } : {}),
      clearPassword,
    };
    try {
      const value = await service.saveSmtp(input);
      sessionBoundary.assert(epoch);
      if (active.current && ticket === sequence.current) {
        adopt(value);
        setNotice(
          value.enabled
            ? c('邮件配置已保存，后续邮件立即使用新配置。')
            : c('邮件配置已保存，邮件服务已停用。'),
        );
      }
    } catch (e) {
      if (active.current && epoch === sessionBoundary.epoch && ticket === sequence.current) {
        setError(message(e, c));
        if (
          writeResultUnknown(e) ||
          (e instanceof ApiError &&
            [
              'MAIL_CONFIGURATION_CONFLICT',
              'MAIL_CONFIG_SAVE_UNKNOWN',
              'CONTENT_BOUNDARY_VIOLATION',
            ].includes(e.code))
        )
          setNeedsRead(true);
      }
    } finally {
      if (ticket === sequence.current) {
        guard.current = false;
        if (active.current && epoch === sessionBoundary.epoch) setBusy('');
      }
    }
  }
  const configured =
      !!saved && (saved.passwordConfigured || !!saved.address || !!saved.username || !!saved.from),
    disabled = !!busy;
  return (
    <section className="op-page op-mail-page">
      <header className="op-page-heading">
        <div>
          <h1>{c('邮件设置')}</h1>
          <p>{c('配置平台邮箱，用于注册、登录验证码和密码找回。')}</p>
        </div>
      </header>
      {!saved ? (
        <Feedback busy={busy === 'load'} error={error} retry={() => void load()} />
      ) : (
        <form className="op-mail-form" onSubmit={save} aria-busy={disabled}>
          <div className="op-mail-status">
            <div>
              <h2>
                {c('SMTP 邮件服务')}{' '}
                <span className="op-tag">
                  {saved.enabled ? c('已启用') : configured ? c('已停用') : c('未配置')}
                </span>
              </h2>
              <p>{c('保存后供后续邮件使用，无需重启服务。')}</p>
            </div>
            <ModelSwitch
              id="smtp-enabled"
              label={c('启用邮件服务')}
              checked={draft.enabled}
              disabled={disabled || clearPassword}
              onChange={() => patch('enabled', !draft.enabled)}
            />
          </div>
          <div className="op-mail-fields">
            <Field
              label={c('SMTP 地址')}
              hint={c('填写服务商提供的 host:port，例如 smtp.example.com:587。')}
            >
              <input
                value={draft.address}
                onChange={(event) => patch('address', event.target.value)}
                required={draft.enabled}
                disabled={disabled}
                autoComplete="off"
                spellCheck={false}
              />
            </Field>
            <Field label={c('加密方式')}>
              <ModelSelect
                label={c('SMTP 加密方式')}
                menuClassName="op-mail-select-menu"
                value={draft.tls}
                options={[
                  { value: 'starttls', label: 'STARTTLS' },
                  { value: 'tls', label: 'TLS' },
                ]}
                disabled={disabled}
                onChange={(value) => patch('tls', value as Draft['tls'])}
              />
            </Field>
            <Field label={c('发送邮箱')}>
              <input
                type="email"
                value={draft.from}
                onChange={(event) => patch('from', event.target.value)}
                required={draft.enabled}
                disabled={disabled}
                autoComplete="off"
              />
            </Field>
            <Field label={c('SMTP 认证用户名')}>
              <input
                value={draft.username}
                onChange={(event) => patch('username', event.target.value)}
                required={draft.enabled}
                disabled={disabled}
                autoComplete="off"
                spellCheck={false}
              />
            </Field>
            <Field
              label={c('SMTP 密码 / 授权码')}
              hint={
                saved.passwordConfigured
                  ? c('已保存密码。留空保持原密码，密码不会回显。')
                  : c('尚未设置密码，启用服务前请填写。')
              }
            >
              <input
                type="password"
                value={password}
                onChange={(event) => {
                  setPassword(event.target.value);
                  setError('');
                  setNotice('');
                }}
                placeholder={
                  clearPassword
                    ? c('保存时将清除原密码')
                    : saved.passwordConfigured
                      ? c('留空保持已保存密码')
                      : c('输入服务商提供的密码或授权码')
                }
                required={draft.enabled && !saved.passwordConfigured}
                disabled={disabled || clearPassword}
                autoComplete="new-password"
              />
            </Field>
          </div>
          {saved.passwordConfigured ? (
            <label className="op-mail-clear">
              <input
                type="checkbox"
                checked={clearPassword}
                disabled={disabled || draft.enabled}
                onChange={(event) => {
                  setClearPassword(event.target.checked);
                  setPassword('');
                  setError('');
                  setNotice('');
                }}
              />
              <span>
                {c('清除已保存密码')}
                <small>{c('须先停用邮件服务，保存后生效。')}</small>
              </span>
            </label>
          ) : null}
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
          {needsRead ? (
            <div className="op-pending" role="status">
              {c('配置尚未确认，请先读取最新配置核对，再继续保存。')}
              <Button
                variant="outline"
                size="sm"
                busy={busy === 'load'}
                disabled={busy === 'save'}
                onClick={() => void load()}
              >
                {c('读取最新配置')}
              </Button>
            </div>
          ) : null}
          <footer>
            <Button type="submit" busy={busy === 'save'} disabled={busy === 'load' || needsRead}>
              {c('保存配置')}
            </Button>
          </footer>
        </form>
      )}
    </section>
  );
}
