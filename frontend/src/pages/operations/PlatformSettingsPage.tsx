import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Upload } from 'lucide-react';
import { ApiError } from '../../api/http-error';
import { writeResultUnknown } from '../../api/errors';
import { sessionBoundary } from '../../api/session-boundary';
import type { PlatformSettings } from '../../api/platform-client';
import { usePlatform } from '../../state/PlatformContext';
import { useSession } from '../../state/SessionContext';
import { PlatformLogo } from '../../components/workspace/PlatformLogo';
import { Button } from '../../components/ui/button';
import { useCatalogText, type CatalogText } from '../../contexts/useCatalogText';
import { Field, Feedback } from './controls';

function message(error: unknown, c: CatalogText) {
  const value = error as { code?: string; message?: string };
  return ({
    PLATFORM_SETTINGS_INVALID: c('名称需为 1–48 个字符，侧栏描述不超过 120 个字符。'),
    INVALID_PLATFORM_LOGO: c('请选择 2MB 以内、尺寸不超过 4096×4096 的 PNG 或 JPEG 图片。'),
    PLATFORM_SETTINGS_CONFLICT: c('配置已被更新，请读取最新配置后再修改。'),
    PLATFORM_SETTINGS_SAVE_UNKNOWN: c('保存结果未确认，请先读取最新配置核对。'),
    PLATFORM_SETTINGS_UNAVAILABLE: c('平台设置暂时无法读取，请稍后重试。'),
    SERVICE_READ_ONLY: c('当前为只读模式，无法修改平台设置。'),
  } as Record<string, string>)[value.code || ''] || value.message || c('平台设置请求失败。');
}

export function PlatformSettingsPage() {
  const { api, adopt } = usePlatform();
  const { epoch: sessionEpoch } = useSession();
  const { c } = useCatalogText();
  const [saved, setSaved] = useState<PlatformSettings | null>(null);
  const [draft, setDraft] = useState<PlatformSettings | null>(null);
  const [busy, setBusy] = useState<'load' | 'upload' | 'save' | ''>('load');
  const [error, setError] = useState(''), [notice, setNotice] = useState('');
  const [needsRead, setNeedsRead] = useState(true);
  const fileInput = useRef<HTMLInputElement>(null);
  const active = useRef(false), guard = useRef(false), sequence = useRef(0);
  const accept = (value: PlatformSettings) => {
    setSaved(value);
    setDraft(value);
    setNeedsRead(false);
    adopt(value);
  };
  async function load() {
    if (guard.current) return;
    guard.current = true;
    const ticket = ++sequence.current, epoch = sessionBoundary.epoch;
    setBusy('load'); setError(''); setNotice('');
    try {
      const value = await api.settings();
      sessionBoundary.assert(epoch);
      if (active.current && ticket === sequence.current) accept(value);
    } catch (e) {
      if (active.current && ticket === sequence.current && epoch === sessionBoundary.epoch) {
        setError(message(e, c)); setNeedsRead(true);
      }
    } finally {
      if (ticket === sequence.current) {
        guard.current = false;
        if (active.current && epoch === sessionBoundary.epoch) setBusy('');
      }
    }
  }
  useEffect(() => {
    active.current = true; guard.current = false;
    setSaved(null); setDraft(null); setNeedsRead(true);
    void load();
    return () => { active.current = false; ++sequence.current; };
  }, [api, sessionEpoch]);
  const patch = (value: Partial<PlatformSettings>) => {
    setDraft((old) => old ? { ...old, ...value } : old);
    setError(''); setNotice('');
  };
  async function upload(file: File) {
    if (guard.current || needsRead) return;
    guard.current = true;
    const ticket = ++sequence.current, epoch = sessionBoundary.epoch;
    setBusy('upload'); setError(''); setNotice('');
    try {
      const value = await api.upload(file);
      sessionBoundary.assert(epoch);
      if (active.current && ticket === sequence.current) patch(value);
    } catch (e) {
      if (active.current && ticket === sequence.current && epoch === sessionBoundary.epoch)
        setError(message(e, c));
    } finally {
      if (ticket === sequence.current) {
        guard.current = false;
        if (active.current && epoch === sessionBoundary.epoch) setBusy('');
      }
    }
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (guard.current || needsRead || !saved || !draft) return;
    guard.current = true;
    const ticket = ++sequence.current, epoch = sessionBoundary.epoch;
    setBusy('save'); setError(''); setNotice('');
    try {
      const value = await api.save({
        revision: saved.revision, name: draft.name,
        sidebarDescription: draft.sidebarDescription, logoId: draft.logoId,
      });
      sessionBoundary.assert(epoch);
      if (active.current && ticket === sequence.current) {
        accept(value); setNotice(c('已保存'));
      }
    } catch (e) {
      if (active.current && ticket === sequence.current && epoch === sessionBoundary.epoch) {
        setError(message(e, c));
        if (writeResultUnknown(e) || (e instanceof ApiError && e.code === 'PLATFORM_SETTINGS_CONFLICT'))
          setNeedsRead(true);
      }
    } finally {
      if (ticket === sequence.current) {
        guard.current = false;
        if (active.current && epoch === sessionBoundary.epoch) setBusy('');
      }
    }
  }
  const changed = !!saved && !!draft && (saved.name !== draft.name.trim() ||
    saved.sidebarDescription !== draft.sidebarDescription.trim() || saved.logoId !== draft.logoId);
  return (
    <section className="op-page op-platform-page">
      <header className="op-page-heading"><h1>{c('平台设置')}</h1></header>
      {!saved || !draft ? <Feedback busy={busy === 'load'} error={error} retry={() => void load()} /> : (
        <form className="op-platform-form" onSubmit={save} aria-busy={!!busy}>
          <div className="op-platform-logo-row">
            <PlatformLogo className="op-platform-logo-preview" settings={draft} />
            <div className="op-platform-logo-controls">
              <div className="op-platform-logo-buttons">
                <Button variant="outline" size="sm" disabled={!!busy || needsRead}
                  busy={busy === 'upload'} onClick={() => fileInput.current?.click()}>
                  <Upload aria-hidden />{c('上传 Logo')}
                </Button>
                <Button variant="ghost" size="sm" disabled={!!busy || needsRead || !draft.logoId}
                  onClick={() => patch({ logoId: '', logoUrl: '' })}>{c('恢复默认')}</Button>
              </div>
              <small>{c('PNG / JPEG，最大 2MB')}</small>
            </div>
            <input ref={fileInput} className="sr-only" type="file" accept="image/png,image/jpeg"
              tabIndex={-1} aria-label={c('平台 Logo 图片')} disabled={!!busy || needsRead}
              onChange={(event) => {
                const file = event.target.files?.[0];
                event.target.value = '';
                if (file) void upload(file);
              }} />
          </div>
          <Field label={c('平台名称')}>
            <input value={draft.name} required maxLength={96} disabled={!!busy}
              onChange={(event) => patch({ name: event.target.value })} />
          </Field>
          <Field label={c('侧栏描述')}>
            <input value={draft.sidebarDescription} maxLength={240} disabled={!!busy}
              onChange={(event) => patch({ sidebarDescription: event.target.value })} />
          </Field>
          {error ? <p className="op-inline-error" role="alert">{error}</p> : null}
          <footer className="op-platform-actions">
            {notice ? <span role="status">{notice}</span> : null}
            {needsRead ? <Button variant="outline" disabled={!!busy} busy={busy === 'load'}
              onClick={() => void load()}>{c('读取最新配置')}</Button> : null}
            <Button type="submit" disabled={!!busy || needsRead || !changed || !draft.name.trim()}
              busy={busy === 'save'}>{c('保存设置')}</Button>
          </footer>
        </form>
      )}
    </section>
  );
}
