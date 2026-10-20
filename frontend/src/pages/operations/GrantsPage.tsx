import { useRef, useState } from 'react';
import { Copy, Plus, Power } from 'lucide-react';
import { useSession } from '../../state/SessionContext';
import { sessionBoundary } from '../../api/session-boundary';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '../../components/ui/dialog';
import { ModelSwitch } from '../model-config/ModelConfigurationControls';
import { Feedback, Field, PageNavigation, useRead } from './controls';
import { dateTime, shanghaiInput } from '../credits/credit-format';
export function GrantsPage() {
  const { service } = useSession(),
    { c } = useCatalogText(),
    [page, setPage] = useState(''),
    [open, setOpen] = useState(false),
    [expires, setExpires] = useState(''),
    [plaintext, setPlaintext] = useState(''),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [copied, setCopied] = useState(false),
    [uncertain, setUncertain] = useState(false);
  const guard = useRef(false),
    dialogEpoch = useRef(0),
    confirmedRegistration = useRef(false);
  const read = useRead(() => service.grants(page), [service, page]),
    setting = useRead(() => service.registration(), [service]);
  if (setting.value) confirmedRegistration.current = setting.value.required;
  const act = async (fn: () => Promise<void>, registration = false) => {
    if (guard.current) return;
    guard.current = true;
    setBusy(true);
    setError('');
    const epoch = sessionBoundary.epoch;
    try {
      await fn();
      sessionBoundary.assert(epoch);
      if (!registration) read.refresh();
    } catch (e) {
      if (epoch === sessionBoundary.epoch) setError((e as Error).message);
    } finally {
      guard.current = false;
      if (epoch === sessionBoundary.epoch) {
        if (registration) setting.refresh();
        setBusy(false);
      }
    }
  };
  const grantColumns = [
    c('授权码标识'),
    c('状态'),
    c('有效期至'),
    c('使用人'),
    c('使用时间'),
    c('操作'),
  ];
  return (
    <section className="op-page">
      <header className="op-page-heading">
        <div>
          <h1>{c('注册授权码')}</h1>
          <p>{c('授权码只控制注册资格，邮箱验证码用于验证邮箱。')}</p>
        </div>
        <Button
          onClick={() => {
            dialogEpoch.current++;
            setOpen(true);
            setError('');
            setPlaintext('');
            setCopied(false);
            setUncertain(false);
          }}
        >
          <Plus />
          {c('创建授权码')}
        </Button>
      </header>
      <div className="op-registration">
        <div>
          <h2>{c('注册需要授权码')}</h2>
          <p>{c('开启后，用户须凭有效授权码注册。')}</p>
        </div>
        <div className="op-registration-control" aria-busy={busy || setting.busy}>
          <span className="op-muted">
            {setting.value
              ? setting.value.required
                ? c('已开启')
                : c('已关闭')
              : setting.error
                ? c('状态未确认')
                : c('读取设置…')}
          </span>
          <ModelSwitch
            id="registration-requires-code"
            label={c('注册需要授权码')}
            checked={setting.value?.required ?? confirmedRegistration.current}
            disabled={busy || setting.busy || !setting.value}
            onChange={() => void act(() => service.setRegistration(!setting.value!.required), true)}
          />
        </div>
      </div>
      <Feedback busy={setting.busy} error={setting.error} retry={setting.refresh} />
      <Feedback busy={read.busy} error={read.error} retry={read.refresh} />
      {error && !open ? (
        <p className="op-inline-error" role="alert">
          {error}
        </p>
      ) : null}
      {read.value ? (
        <>
          <div className="op-table-scroll" tabIndex={0} aria-label={c('注册授权码列表')}>
            <table className="op-table">
              <thead>
                <tr>
                  {grantColumns.map((x) => (
                    <th key={x}>{x}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {read.value.items.map((x) => (
                  <tr key={x.identity}>
                    <td>{x.identity}</td>
                    <td>{c(x.stateLabel)}</td>
                    <td>{dateTime(x.expiresAt)}</td>
                    <td>{x.usedBy || '—'}</td>
                    <td>{dateTime(x.usedAt)}</td>
                    <td>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={busy || !x.canDisable}
                        onClick={() => void act(() => service.disableGrant(x.identity))}
                      >
                        <Power aria-hidden />
                        {c('停用')}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Feedback busy={false} error="" empty={!read.value.items.length} retry={read.refresh} />
          <PageNavigation page={read.value} busy={read.busy} onPage={setPage} />
        </>
      ) : null}
      <Dialog
        open={open}
        onOpenChange={(v) => {
          if (!busy) {
            setOpen(v);
            if (!v) {
              dialogEpoch.current++;
              setPlaintext('');
              setExpires('');
            }
          }
        }}
      >
        <DialogContent variant="form" className="op-dialog" closeDisabled={busy}>
          <DialogTitle>{c('创建注册授权码')}</DialogTitle>
          <DialogDescription>{c('单次使用；明文仅在本次创建成功时显示。')}</DialogDescription>
          {plaintext ? (
            <div className="op-created-code">
              <p>{c('请复制并妥善交付，关闭后无法回查明文。')}</p>
              <code>{plaintext}</code>
              <Button
                variant="outline"
                onClick={() =>
                  void navigator.clipboard
                    .writeText(plaintext)
                    .then(() => setCopied(true))
                    .catch(() => setError(c('复制失败，请手动选择复制。')))
                }
              >
                <Copy />
                {copied ? c('已复制') : c('复制授权码')}
              </Button>
            </div>
          ) : (
            <form
              onSubmit={(e) => {
                e.preventDefault();
                const ticket = dialogEpoch.current;
                void act(async () => {
                  setUncertain(true);
                  const result = await service.createGrant(shanghaiInput(expires));
                  if (ticket === dialogEpoch.current) {
                    setPlaintext(result.plaintext);
                    setUncertain(false);
                  }
                });
              }}
            >
              <Field label={c('有效期至（北京时间）')}>
                <input
                  type="datetime-local"
                  value={expires}
                  onChange={(e) => setExpires(e.target.value)}
                  required
                  disabled={busy || uncertain}
                />
              </Field>
              {uncertain && !busy ? (
                <p className="op-inline-error" role="alert">
                  {c('创建结果未确认。请回读列表；不要重复创建。明文未送达需由后端处理该次交付。')}
                </p>
              ) : null}
              <footer>
                <Button type="submit" busy={busy} disabled={uncertain}>
                  {c('创建')}
                </Button>
              </footer>
            </form>
          )}
          {error ? (
            <p className="op-inline-error" role="alert">
              {error}
            </p>
          ) : null}
        </DialogContent>
      </Dialog>
    </section>
  );
}
