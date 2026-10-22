import { useId, useState, type FormEvent } from 'react';
import {
  paymentApi,
  type PaymentAPI,
  type OperationsPaymentPackage,
  type PaymentSettings,
} from '../../api/payment-client';
import {
  formatPaymentAmount,
  paymentAmountInput,
  paymentCreditsInput,
} from '../../domain/payment-format';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '../../components/ui/dialog';
import { Feedback, Field, useRead } from './controls';
import { ModelSwitch } from '../model-config/ModelConfigurationControls';
import { dateTime, formatInteger } from '../credits/credit-format';
import { PaymentMessages, usePaymentAction, usePaymentEpoch } from './payment-controls';
export function OperationsPaymentPackagesPage({ api = paymentApi }: { api?: PaymentAPI }) {
  const epoch = usePaymentEpoch();
  return <PackagesScope key={epoch} api={api} />;
}
function PackagesScope({ api }: { api: PaymentAPI }) {
  const { c } = useCatalogText(),
    read = useRead((signal) => api.packages(signal), [api]),
    settings = useRead((signal) => api.settings(signal), [api]),
    [editors, setEditors] = useState<
      Record<
        string,
        { saved: OperationsPaymentPackage | null; id: string; settled: boolean; generation: number }
      >
    >({}),
    [activeEditor, setActiveEditor] = useState<string | null>(null),
    [settingsOpen, setSettingsOpen] = useState(false);
  // 编辑状态留在 Portal 外；关闭或切换档位不能丢掉待核对草稿、解除 CAS/未知提交保护。
  function editPackage(saved: OperationsPaymentPackage | null) {
    const key = saved
      ? Object.entries(editors).find(([, draft]) => draft.id === saved.id)?.[0] ||
        'package:' + saved.id
      : 'new';
    const id = saved?.id ||
      (editors[key] && !editors[key].settled ? editors[key].id : crypto.randomUUID());
    setEditors((current) =>
      current[key] && !current[key].settled
        ? current
        : {
            ...current,
            [key]: {
              saved,
              id,
              settled: false,
              generation: (current[key]?.generation || 0) + 1,
            },
          },
    );
    setActiveEditor(key);
  }
  return (
    <section className="op-page op-payment-page">
      <header className="op-page-heading">
        <div>
          <h1>{c('充值档位与限额')}</h1>
          <p>{c('修改只影响新订单，已下订单继续使用冻结的价格与积分。')}</p>
        </div>
        <div className="op-payment-heading-actions">
          <Button
            variant="outline"
            disabled={!settings.value}
            onClick={() => setSettingsOpen(true)}
          >
            {c('设置单日限额')}
          </Button>
          <Button disabled={!read.value} onClick={() => editPackage(null)}>{c('新增充值档位')}</Button>
        </div>
      </header>
      <Feedback busy={settings.busy} error={settings.error} retry={settings.refresh} />
      {settings.value ? (
        <SettingsEditor
          key={settings.value.revision}
          api={api}
          saved={settings.value}
          open={settingsOpen}
          onOpenChange={setSettingsOpen}
          onSaved={() => {
            setSettingsOpen(false);
            settings.refresh();
          }}
        />
      ) : null}
      <Feedback busy={read.busy} error={read.error} retry={read.refresh} />
      {read.value ? (
        <div className="op-table-scroll" tabIndex={0} aria-label={c('充值档位列表')}>
          <table className="op-table">
            <thead>
              <tr>
                {[
                  c('档位名称'),
                  c('原价'),
                  c('实付金额'),
                  c('充值积分'),
                  c('赠送积分'),
                  c('状态'),
                  c('排序'),
                  c('更新时间'),
                  c('操作'),
                ].map((v) => (
                  <th key={v}>{v}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {!read.value.length ? (
                <tr>
                  <td colSpan={9} className="op-payment-empty" role="status">
                    {c('当前条件下暂无记录')}
                  </td>
                </tr>
              ) : null}
              {read.value.map((v) => (
                <tr key={v.id}>
                  <td>
                    {v.name || c('未命名')}
                    {v.testOnly ? <small>{c('仅非正式环境运营可见')}</small> : null}
                  </td>
                  <td className="op-payment-money">{formatPaymentAmount(v.originalAmountFen)}</td>
                  <td className="op-payment-money">{formatPaymentAmount(v.amountFen)}</td>
                  <td>{formatInteger(String(v.rechargeCredits))}</td>
                  <td>{formatInteger(String(v.giftCredits))}</td>
                  <td>{v.enabled ? c('已上架') : c('已下架')}</td>
                  <td>{v.sort}</td>
                  <td>{dateTime(v.updatedAt)}</td>
                  <td>
                    <Button size="sm" variant="outline" onClick={() => editPackage(v)}>
                      {c('编辑档位')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      {Object.entries(editors).map(([key, draft]) => (
        <PackageEditor
          key={key + ':' + draft.generation}
          api={api}
          saved={draft.saved}
          id={draft.id}
          open={activeEditor === key}
          onOpenChange={(visible) => {
            if (!visible) setActiveEditor((current) => (current === key ? null : current));
          }}
          onSaved={() => {
            setActiveEditor((current) => (current === key ? null : current));
            setEditors((current) => ({
              ...current,
              [key]: { ...current[key], settled: true },
            }));
            read.refresh();
          }}
        />
      ))}
    </section>
  );
}
function SettingsEditor({
  api,
  saved,
  open,
  onOpenChange,
  onSaved,
}: {
  api: PaymentAPI;
  saved: PaymentSettings;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
}) {
  const { c } = useCatalogText(),
    action = usePaymentAction(),
    base = saved,
    [amount, setAmount] = useState(formatPaymentAmount(saved.dailyLimitFen).slice(1)),
    [blocked, setBlocked] = useState(false);
  async function save(e: FormEvent) {
    e.preventDefault();
    if (blocked) return;
    let dailyLimitFen: number;
    try {
      dailyLimitFen = paymentAmountInput(amount);
    } catch (e) {
      action.setError((e as Error).message);
      return;
    }
    await action.run(async (current) => {
      try {
        await api.saveSettings({
          revision: base.revision,
          dailyLimitFen,
        });
        if (current()) {
          action.setNotice(c('单日限额已保存。'));
          onSaved();
        }
      } catch {
        if (current()) setBlocked(true);
        throw new Error(c('保存结果暂未确认，请刷新页面后核对。'));
      }
    });
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent variant="form" className="op-dialog op-payment-dialog">
        <DialogTitle>{c('每位下单人单日限额')}</DialogTitle>
        <DialogDescription>
          {c('按北京时间累计个人与团队成功支付金额，0 表示不限。')}
        </DialogDescription>
        <form className="op-payment-form" onSubmit={save}>
          <Field label={c('单日限额（元）')}>
            <input
              className="jm-field-input"
              required
              inputMode="decimal"
              value={amount}
              disabled={action.busy || blocked}
              onChange={(e) => setAmount(e.target.value)}
            />
          </Field>
          <div className="op-payment-buttons">
            <Button type="submit" busy={action.busy} disabled={blocked}>
              {c('保存单日限额')}
            </Button>
          </div>
        </form>
        <PaymentMessages error={action.error} notice={action.notice} />
      </DialogContent>
    </Dialog>
  );
}
function PackageEditor({
  api,
  saved,
  open,
  onOpenChange,
  id,
  onSaved,
}: {
  api: PaymentAPI;
  saved: OperationsPaymentPackage | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  id: string;
  onSaved: () => void;
}) {
  const switchId = useId();
  const { c } = useCatalogText(),
    action = usePaymentAction(),
    base = saved,
    [name, setName] = useState(saved?.name || ''),
    [originalAmount, setOriginalAmount] = useState(saved ? formatPaymentAmount(saved.originalAmountFen).slice(1) : ''),
    [discountAmount, setDiscountAmount] = useState(saved && saved.amountFen < saved.originalAmountFen ? formatPaymentAmount(saved.amountFen).slice(1) : ''),
    [recharge, setRecharge] = useState(saved ? String(saved.rechargeCredits) : ''),
    [gift, setGift] = useState(saved ? String(saved.giftCredits) : '0'),
    [enabled, setEnabled] = useState(saved?.enabled || false),
    [testOnly, setTestOnly] = useState(saved?.testOnly || false),
    [sort, setSort] = useState(String(saved?.sort || 0)),
    [blocked, setBlocked] = useState(false);
  async function save(e: FormEvent) {
    e.preventDefault();
    if (blocked) return;
    let originalAmountFen: number,
      amountFen: number,
      rechargeCredits: number,
      giftCredits: number,
      position: number;
    try {
      originalAmountFen = paymentAmountInput(originalAmount);
      amountFen = discountAmount.trim() ? paymentAmountInput(discountAmount) : originalAmountFen;
      if (originalAmountFen === 0 || amountFen === 0) throw new Error(c('档位金额须大于 0。'));
      if (amountFen > originalAmountFen) throw new Error(c('优惠价不得高于原价。'));
      const cleanName = name.trim();
      if (!cleanName || Array.from(cleanName).length > 48 || /[\p{Cc}\uD800-\uDFFF]/u.test(cleanName))
        throw new Error(c('请填写 1–48 个字符的档位名称，不含控制字符。'));
      rechargeCredits = paymentCreditsInput(recharge);
      giftCredits = paymentCreditsInput(gift, true);
      if (
        !/^-?(0|[1-9][0-9]*)$/.test(sort) ||
        Number(sort) < -2147483648 ||
        Number(sort) > 2147483647
      )
        throw new Error(c('排序须为有效的 32 位整数。'));
      position = Number(sort);
    } catch (e) {
      action.setError((e as Error).message);
      return;
    }
    await action.run(async (current) => {
      try {
        await api.savePackage({
          id,
          name: name.trim(),
          originalAmountFen,
          revision: base?.revision || 0,
          amountFen,
          rechargeCredits,
          giftCredits,
          enabled,
          testOnly,
          sort: position,
        });
        if (current()) onSaved();
      } catch {
        if (current()) setBlocked(true);
        throw new Error(c('保存结果暂未确认，请刷新页面后核对。'));
      }
    });
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        variant="settings"
        className="op-dialog op-payment-dialog op-payment-package-dialog"
      >
        <DialogTitle>{saved ? c('编辑充值档位') : c('新增充值档位')}</DialogTitle>
        <DialogDescription>
          {c('名称展示在用户充值卡片上，价格、积分与状态一并保存。')}
        </DialogDescription>
        <form className="op-payment-form op-payment-package-form" onSubmit={save}>
          <div className="op-payment-form-wide">
            <Field label={c('档位名称')} hint={c('显示在充值卡片左上角，例如“月度灵感包”。')}>
              <input
                className="jm-field-input"
                required
                value={name}
                disabled={action.busy || blocked}
                onChange={(e) => setName(e.target.value)}
              />
            </Field>
          </div>
          <Field label={c('原价（元）')}>
            <input
              className="jm-field-input"
              required
              inputMode="decimal"
              value={originalAmount}
              disabled={action.busy || blocked}
              onChange={(e) => setOriginalAmount(e.target.value)}
            />
          </Field>
          <Field label={c('优惠价（元）')} hint={c('选填，留空按原价付款。')}>
            <input
              className="jm-field-input"
              inputMode="decimal"
              value={discountAmount}
              disabled={action.busy || blocked}
              onChange={(e) => setDiscountAmount(e.target.value)}
            />
          </Field>
          <Field label={c('充值积分数')}>
            <input
              className="jm-field-input"
              required
              inputMode="numeric"
              value={recharge}
              disabled={action.busy || blocked}
              onChange={(e) => setRecharge(e.target.value)}
            />
          </Field>
          <Field label={c('赠送积分数')}>
            <input
              className="jm-field-input"
              required
              inputMode="numeric"
              value={gift}
              disabled={action.busy || blocked}
              onChange={(e) => setGift(e.target.value)}
            />
          </Field>
          <Field label={c('排序')}>
            <input
              className="jm-field-input"
              required
              inputMode="numeric"
              value={sort}
              disabled={action.busy || blocked}
              onChange={(e) => setSort(e.target.value)}
            />
          </Field>
          <div className="op-payment-package-checks">
            <div className="op-payment-switch">
              <label htmlFor={switchId + '-enabled'}>{c('上架档位')}</label>
              <ModelSwitch
                id={switchId + '-enabled'}
                label={c('上架档位')}
                checked={enabled}
                disabled={action.busy || blocked}
                onChange={() => setEnabled((value) => !value)}
              />
            </div>
            <div className="op-payment-switch">
              <label htmlFor={switchId + '-test-only'}>{c('仅非正式环境运营可见')}</label>
              <ModelSwitch
                id={switchId + '-test-only'}
                label={c('仅非正式环境运营可见')}
                checked={testOnly}
                disabled={action.busy || blocked}
                onChange={() => setTestOnly((value) => !value)}
              />
            </div>
          </div>
          <div className="op-payment-buttons op-payment-form-wide">
            <Button type="submit" busy={action.busy} disabled={blocked}>
              {c('保存充值档位')}
            </Button>
          </div>
        </form>
        <PaymentMessages error={action.error} notice={action.notice} />
      </DialogContent>
    </Dialog>
  );
}
