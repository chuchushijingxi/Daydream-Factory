import { useEffect, useState } from 'react';
import type { ModelSettingsInput } from '../../api/types';
import { useCatalogText } from '../../contexts/useCatalogText';

type Behavior = NonNullable<ModelSettingsInput['mockBehavior']>;
const valid = (raw: string, max: number) =>
  raw.trim() !== '' && Number.isInteger(Number(raw)) && Number(raw) >= 0 && Number(raw) <= max;
export function ModelMockBehaviorFields({
  value,
  index,
  disabled,
  onChange,
}: {
  value: Behavior | null | undefined;
  index: number;
  disabled: boolean;
  onChange: (value: Behavior) => void;
}) {
  const { c } = useCatalogText();
  const [delay, setDelay] = useState(value ? String(value.delaySeconds) : ''),
    [failure, setFailure] = useState(value ? String(value.failurePercent) : '');
  useEffect(() => setDelay(value ? String(value.delaySeconds) : ''), [value?.delaySeconds]);
  useEffect(() => setFailure(value ? String(value.failurePercent) : ''), [value?.failurePercent]);
  if (!value)
    return (
      <p className="mc-form-error" role="alert">
        {c('模拟设置读取失败，请重新打开页面。')}
      </p>
    );
  return (
    <section className="mc-mock-behavior" aria-label={c('工作台模拟行为')}>
      <h4>{c('工作台模拟行为')}</h4>
      <div className="mc-fields-grid">
        <div className="mc-field">
          <label htmlFor={'mc-mock-delay-' + index}>{c('模拟延迟（秒）')}</label>
          <input
            id={'mc-mock-delay-' + index}
            type="number"
            min={0}
            max={60}
            step={1}
            required
            value={delay}
            disabled={disabled}
            aria-invalid={!valid(delay, 60) || undefined}
            onChange={(event) => {
              const raw = event.target.value;
              setDelay(raw);
              if (valid(raw, 60)) onChange({ ...value, delaySeconds: Number(raw) });
            }}
          />
          <p className="mc-reason">{c('0～60秒，0表示不额外延迟。')}</p>
        </div>
        <div className="mc-field">
          <label htmlFor={'mc-mock-failure-' + index}>{c('失败概率（%）')}</label>
          <input
            id={'mc-mock-failure-' + index}
            type="number"
            min={0}
            max={100}
            step={1}
            required
            value={failure}
            disabled={disabled}
            aria-invalid={!valid(failure, 100) || undefined}
            onChange={(event) => {
              const raw = event.target.value;
              setFailure(raw);
              if (valid(raw, 100)) onChange({ ...value, failurePercent: Number(raw) });
            }}
          />
          <p className="mc-reason">{c('0不失败，100必定失败；其余为每个新任务的失败概率。')}</p>
        </div>
      </div>
      <p className="mc-reason">{c('保存后在工作台选择此模型时生效；运行中的任务保持启动设置。')}</p>
    </section>
  );
}
