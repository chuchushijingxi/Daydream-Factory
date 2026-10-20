import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useSession } from '../../state/SessionContext';
import { sessionBoundary } from '../../api/session-boundary';
import { ApiError } from '../../api/http-error';
import { writeResultUnknown } from '../../api/errors';
import type { CreditPolicyView } from '../../api/administration';
import { Button } from '../../components/ui/button';
import { Feedback, Field } from '../operations/controls';
import { integer } from './credit-format';
import { useCatalogText } from '../../contexts/useCatalogText';
import { translate } from '../../contexts/translate';
function message(error: unknown) {
  const value = error as { code?: string; message?: string };
  const messages: Record<string, string> = {
    CREDIT_POLICY_INVALID: translate('赠送积分须为安全范围内的非负整数。'),
    CREDIT_POLICY_CONFLICT: translate('赠送设置已被更新，请读取最新配置后再修改。'),
    CREDIT_POLICY_SAVE_UNKNOWN: translate('保存结果未确认，请读取最新配置核对。'),
  };
  return messages[value.code || ''] || value.message || translate('赠送设置请求失败。');
}
export function CreditPolicyPage() {
  const { c } = useCatalogText();
  const { service } = useSession();
  const [saved, setSaved] = useState<CreditPolicyView | null>(null),
    [registration, setRegistration] = useState('0'),
    [daily, setDaily] = useState('0'),
    [busy, setBusy] = useState<'load' | 'save' | ''>('load'),
    [needsRead, setNeedsRead] = useState(true),
    [error, setError] = useState(''),
    [notice, setNotice] = useState('');
  const active = useRef(false),
    guard = useRef(false),
    sequence = useRef(0);
  const adopt = (value: CreditPolicyView) => {
    setSaved(value);
    setRegistration(value.registrationGift);
    setDaily(value.dailyGift);
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
      const value = await service.creditPolicy();
      sessionBoundary.assert(epoch);
      if (active.current && ticket === sequence.current) {
        adopt(value);
        setNotice(first ? '' : c('已读取最新设置，请核对后继续。'));
      }
    } catch (e) {
      if (active.current && ticket === sequence.current && epoch === sessionBoundary.epoch) {
        setNeedsRead(true);
        setError(message(e));
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
  async function save(event: FormEvent) {
    event.preventDefault();
    if (guard.current || needsRead || !saved) return;
    let registrationGift: number, dailyGift: number;
    try {
      for (const value of [registration, daily]) {
        integer(value);
        if (BigInt(value) > 9007199254740991n)
          throw new Error(c('积分不能超过9,007,199,254,740,991。'));
      }
      registrationGift = Number(registration);
      dailyGift = Number(daily);
    } catch (e) {
      setError((e as Error).message);
      return;
    }
    guard.current = true;
    const ticket = ++sequence.current,
      epoch = sessionBoundary.epoch;
    setBusy('save');
    setError('');
    setNotice('');
    try {
      const value = await service.saveCreditPolicy({
        revision: saved.revision,
        registrationGift,
        dailyGift,
      });
      sessionBoundary.assert(epoch);
      if (active.current && ticket === sequence.current) {
        adopt(value);
        setNotice(c('赠送设置已保存，后续注册及尚未领取的当日赠送按新设置执行。'));
      }
    } catch (e) {
      if (active.current && ticket === sequence.current && epoch === sessionBoundary.epoch) {
        setError(message(e));
        if (
          writeResultUnknown(e) ||
          (e instanceof ApiError &&
            ['CREDIT_POLICY_CONFLICT', 'CREDIT_POLICY_SAVE_UNKNOWN'].includes(e.code))
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
  return (
    <section className="op-page">
      <header className="op-page-heading">
        <div>
          <h1>{c('积分赠送设置')}</h1>
          <p>{c('设置新用户注册和每天进入工作台时赠送的积分。')}</p>
        </div>
      </header>
      {!saved ? (
        <Feedback busy={busy === 'load'} error={error} retry={() => void load()} />
      ) : (
        <form className="op-credit-policy-form" onSubmit={save} aria-busy={!!busy}>
          <h2>{c('自动赠送')}</h2>
          <p>{c('赠送积分记入“赠送及免费积分”，数值为 0 时不发放积分。')}</p>
          <div className="op-credit-policy-fields">
            <Field
              label={c('新用户注册赠送')}
              hint={c('每位新用户完成注册时发放一次，不补发给已有用户。')}
            >
              <input
                inputMode="numeric"
                aria-label={c('新用户注册赠送')}
                pattern="(0|[1-9][0-9]*)"
                value={registration}
                onChange={(event) => {
                  setRegistration(event.target.value);
                  setError('');
                  setNotice('');
                }}
                disabled={!!busy}
                required
              />
            </Field>
            <Field
              label={c('每日赠送')}
              hint={c('用户登录后进入工作台时领取；0 不算领取，当日开启后仍可领。')}
            >
              <input
                inputMode="numeric"
                aria-label={c('每日赠送')}
                pattern="(0|[1-9][0-9]*)"
                value={daily}
                onChange={(event) => {
                  setDaily(event.target.value);
                  setError('');
                  setNotice('');
                }}
                disabled={!!busy}
                required
              />
            </Field>
          </div>
          <div className="op-credit-policy-rules">
            <p>{c('同一天实际领取一次，已领积分不重复补发；不补过去日期，不在凌晨全员发放。')}</p>
            <p>{c('每日积分次日北京时间 00:00 到期回收，跨天退款不延长有效期。')}</p>
            <p>{c('优先使用每日积分，再用其他赠送和充值；注册及运营赠送不设到期。')}</p>
          </div>
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
              {c('保存结果尚未确认，请先读取最新设置核对，再继续保存。')}
              <Button
                variant="outline"
                size="sm"
                busy={busy === 'load'}
                disabled={busy === 'save'}
                onClick={() => void load()}
              >
                {c('读取最新设置')}
              </Button>
            </div>
          ) : null}
          <footer>
            <Button type="submit" busy={busy === 'save'} disabled={busy === 'load' || needsRead}>
              {c('保存设置')}
            </Button>
          </footer>
        </form>
      )}
    </section>
  );
}
