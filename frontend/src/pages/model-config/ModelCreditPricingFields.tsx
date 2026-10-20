import { useEffect, useId, useState } from 'react';
import type { CreditPriceDTO } from '../../api/administration-types';
import type { MediaProfile } from '../../api/types';
import { Field } from '../operations/controls';
import { shanghaiInput } from '../credits/credit-format';
import { ModelSwitch } from './ModelConfigurationControls';
import { highestMediaResolution } from '../../features/media/media-resolution-options';
import { DateTimePicker } from '../../components/ui/date-time-picker';
import { useCatalogText } from '../../contexts/useCatalogText';
function localInput(value: string | null) {
  return value
    ? new Date(new Date(value).getTime() + 8 * 3600 * 1000).toISOString().slice(0, -1)
    : '';
}
function RateInput({
  label,
  value,
  disabled,
  required,
  compact,
  onChange,
}: {
  label: string;
  value: number | undefined;
  disabled: boolean;
  required?: boolean;
  compact?: boolean;
  onChange: (n: number | undefined) => void;
}) {
  const { c } = useCatalogText();
  const [text, setText] = useState(value !== undefined && value >= 0 ? String(value) : '');
  useEffect(() => setText(value !== undefined && value >= 0 ? String(value) : ''), [value]);
  const input = (
    <input
      aria-label={label}
      inputMode="numeric"
      value={text}
      disabled={disabled}
      placeholder={c('未配置')}
      required={required}
      onChange={(e) => {
        const raw = e.target.value;
        setText(raw);
        const valid = !raw || (/^(0|[1-9][0-9]*)$/.test(raw) && BigInt(raw) <= 9007199254740991n);
        e.target.setCustomValidity(valid ? '' : c('请输入安全范围内的非负整数积分。'));
        if (valid) onChange(raw ? Number(raw) : undefined);
      }}
    />
  );
  return compact ? (
    <div className="mc-tier-price">{input}</div>
  ) : (
    <Field label={label}>{input}</Field>
  );
}
export function ModelCreditPricingFields({
  category,
  profile,
  value,
  disabled,
  onChange,
  resolutionEnabled,
  onResolutionEnabledChange,
}: {
  resolutionEnabled?: Record<string, boolean>;
  onResolutionEnabledChange?: (value: Record<string, boolean>) => void;
  category: 'analysis' | 'image' | 'video';
  profile?: MediaProfile | null;
  value: CreditPriceDTO | null | undefined;
  disabled: boolean;
  onChange: (value: CreditPriceDTO | null) => void;
}) {
  const { c } = useCatalogText();
  const id = useId();
  // 计费分母按单位取主 catalog 的固定文案（字/张/秒由 lead-core 维护）。
  const unitPriceLabel =
    category === 'analysis' ? c('积分/100 字') : category === 'image' ? c('积分/张') : c('积分/秒');
  const base: CreditPriceDTO = value || {
    unitPrice: category === 'analysis' ? -1 : 0,
    freeEnabled: false,
    freeStart: null,
    freeEnd: null,
  };
  const resolutions = profile?.resolutions || [],
    defaultResolution = profile?.defaultResolution;
  const enabled = Object.fromEntries(
    resolutions.map((r) => [
      r.id,
      resolutionEnabled
        ? !!resolutionEnabled[r.id]
        : !!value && (r.id === defaultResolution || value.resolutionPrices?.[r.id] !== undefined),
    ]),
  );
  const highest =
    category === 'image' ? highestMediaResolution(resolutions.filter((r) => enabled[r.id])) : '';
  const updateRate = (resolution: string, n: number | undefined) => {
    if (resolution === defaultResolution) {
      if (n === undefined && !Object.keys(base.resolutionPrices || {}).length) {
        onChange(null);
        return;
      }
      const prices = { ...base.resolutionPrices };
      delete prices[resolution];
      onChange({
        ...base,
        resolutionPrices: Object.keys(prices).length ? prices : undefined,
        unitPrice: n ?? -1,
      });
    } else {
      const prices = { ...base.resolutionPrices };
      if (n === undefined) delete prices[resolution];
      else prices[resolution] = n;
      onChange({ ...base, resolutionPrices: Object.keys(prices).length ? prices : undefined });
    }
  };
  return (
    <section className="op-price-fields mc-tier-settings" aria-label={c('用户积分定价')}>
      <h4>{resolutions.length ? c('生成档位') : c('用户积分定价')}</h4>
      {resolutions.length ? (
        <div className="mc-tier-table">
          <div className="mc-tier-heading" aria-hidden>
            <span>{c('分辨率')}</span>
            <span>{unitPriceLabel}</span>
            <span>{c('启用')}</span>
          </div>
          {resolutions.map((r) => (
            <div key={r.id} className="mc-tier-row" data-enabled={enabled[r.id]}>
              <div className="mc-tier-name">
                <span>{r.label}</span>
                {r.id === highest ? <span className="mc-tier-default">{c('默认')}</span> : null}
              </div>
              <RateInput
                compact
                label={c('{tier}（{unit}）', { tier: r.label, unit: unitPriceLabel })}
                value={
                  value?.resolutionPrices?.[r.id] ??
                  (r.id === defaultResolution ? value?.unitPrice : undefined)
                }
                required={enabled[r.id] || (r.id === defaultResolution && !!value)}
                disabled={disabled}
                onChange={(n) => updateRate(r.id, n)}
              />
              <ModelSwitch
                id={`tier-${id}-${r.id}`}
                checked={enabled[r.id]}
                disabled={disabled}
                label={c('启用 {tier} 档位', { tier: r.label })}
                onChange={() => onResolutionEnabledChange?.({ ...enabled, [r.id]: !enabled[r.id] })}
              />
            </div>
          ))}
          {category === 'image' ? <p className="mc-tier-hint">{c('默认使用最高已开启分辨率')}</p> : null}
        </div>
      ) : (
        <RateInput
          label={unitPriceLabel}
          value={value?.unitPrice}
          disabled={disabled}
          onChange={(n) => onChange(n === undefined ? null : { ...base, unitPrice: n })}
        />
      )}
      <div className="mc-promotion-fields">
        <div className="op-field mc-promotion-switch">
          <span>{c('限免活动')}</span>
          <ModelSwitch
            id={'credit-free-' + id}
            checked={base.freeEnabled}
            disabled={disabled || !value}
            label={c('启用限免活动')}
            onChange={() => onChange({ ...base, freeEnabled: !base.freeEnabled })}
          />
        </div>
        <div className="op-field">
          <span>{c('开始时间')}</span>
          <DateTimePicker
            label={c('限免开始')}
            disabled={disabled || !value}
            required={base.freeEnabled}
            value={localInput(base.freeStart)}
            onChange={(next) => onChange({ ...base, freeStart: next ? shanghaiInput(next) : null })}
          />
        </div>
        <div className="op-field">
          <span>{c('结束时间')}</span>
          <DateTimePicker
            label={c('限免结束')}
            disabled={disabled || !value}
            required={base.freeEnabled}
            min={localInput(base.freeStart)}
            value={localInput(base.freeEnd)}
            onChange={(next) => onChange({ ...base, freeEnd: next ? shanghaiInput(next) : null })}
          />
        </div>
      </div>
    </section>
  );
}
