import type { TeamPatch, TeamView, ModelDiscount } from '../../../api/spaces-types';
import { Button } from '../../../components/ui/button';
import { useCatalogText } from '../../../contexts/useCatalogText';
import { Choice, Field } from '../controls';
import { DateTimePicker } from '../../../components/ui/date-time-picker';
import { shanghaiInput } from '../../credits/credit-format';
import { numberInput } from '../../team/team-ui';
export type ContractDraft = {
  memberLimit: string;
  expiresAt: string;
  grantAmount: string;
  modelDiscounts: ModelDiscount[];
};
export function shanghaiDraft(value: string) {
  const parts = new Intl.DateTimeFormat('sv-SE', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(new Date(value));
  return parts.replace(' ', 'T');
}

export function buildTeamPatch(
  saved: TeamView,
  draft: ContractDraft,
  ownerId: string,
  disabled: boolean,
): TeamPatch {
  return {
    revision: saved.revision,
    ...(draft.expiresAt === shanghaiDraft(saved.expiresAt)
      ? {}
      : { expiresAt: shanghaiInput(draft.expiresAt) }),
    ...(draft.memberLimit === String(saved.memberLimit)
      ? {}
      : { memberLimit: numberInput(draft.memberLimit, 1) }),
    ...(draft.grantAmount === String(saved.grantAmount)
      ? {}
      : { grantAmount: numberInput(draft.grantAmount) }),
    ...(ownerId.trim() === saved.ownerId ? {} : { ownerId: ownerId.trim() }),
    ...(disabled === saved.disabled ? {} : { disabled }),
    ...(JSON.stringify(draft.modelDiscounts) === JSON.stringify(saved.modelDiscounts)
      ? {}
      : { modelDiscounts: draft.modelDiscounts }),
  };
}

// 折扣行须先选模型；未选择时禁止提交，避免把空模型交给服务端。
export const discountsComplete = (draft: ContractDraft) =>
  draft.modelDiscounts.every((item) => !!item.modelInstanceId);
export function TeamContractFields({
  value,
  patch,
  models,
  disabled,
}: {
  value: ContractDraft;
  patch: (value: Partial<ContractDraft>) => void;
  models: { id: string; name: string }[];
  disabled: boolean;
}) {
  const { c } = useCatalogText();
  const update = (index: number, next: Partial<ModelDiscount>) =>
    patch({
      modelDiscounts: value.modelDiscounts.map((item, i) => (i === index ? { ...item, ...next } : item)),
    });
  return (
    <>
      <div className="team-contract-grid">
        <Field label={c('人数上限')} hint={c('含所有者')}>
          <input
            required
            type="number"
            inputMode="numeric"
            min="1"
            step="1"
            value={value.memberLimit}
            onChange={(e) => patch({ memberLimit: e.target.value })}
            disabled={disabled}
          />
        </Field>
        <Field label={c('到期时间（北京时间）')}>
          <DateTimePicker
            label={c('到期时间（北京时间）')}
            required
            value={value.expiresAt}
            onChange={(expiresAt) => patch({ expiresAt })}
            disabled={disabled}
          />
        </Field>
        <Field label={c('每 30 天发放积分')} hint={c('0 表示不自动发放')}>
          <input
            required
            type="number"
            inputMode="numeric"
            min="0"
            step="1"
            value={value.grantAmount}
            onChange={(e) => patch({ grantAmount: e.target.value })}
            disabled={disabled}
          />
        </Field>
      </div>
      <div className="team-discounts" aria-label={c('模型折扣')} role="group">
        <div className="team-discounts-head">
          <div>
            <h3>{c('模型折扣')}</h3>
            <p className="op-muted">{c('100% 为原价，1–100% 按模型设置；未配置的模型保持原价。')}</p>
          </div>
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={disabled}
            onClick={() =>
              patch({
                modelDiscounts: [...value.modelDiscounts, { modelInstanceId: '', percent: 100 }],
              })
            }
          >
            {c('添加模型折扣')}
          </Button>
        </div>
        {value.modelDiscounts.length ? (
          <ul className="team-discount-list">
            {value.modelDiscounts.map((discount, index) => {
              const known = models.some((model) => model.id === discount.modelInstanceId);
              return (
                <li key={index}>
                  <Choice
                    label={c('模型')}
                    value={discount.modelInstanceId}
                    disabled={disabled}
                    className="team-discount-model"
                    options={[
                      ...(!known && discount.modelInstanceId
                        ? [{ value: discount.modelInstanceId, label: discount.modelInstanceId }]
                        : []),
                      ...models.map((model) => ({ value: model.id, label: model.name })),
                    ]}
                    onChange={(modelInstanceId) => update(index, { modelInstanceId })}
                  />
                  <label className="team-discount-percent">
                    <input
                      aria-label={c('折扣百分比')}
                      required
                      type="number"
                      inputMode="numeric"
                      min="1"
                      max="100"
                      step="1"
                      value={discount.percent}
                      disabled={disabled}
                      onChange={(e) => update(index, { percent: Number(e.target.value) })}
                    />
                    <span aria-hidden>%</span>
                  </label>
                  <Button
                    variant="ghost"
                    size="sm"
                    type="button"
                    disabled={disabled}
                    onClick={() =>
                      patch({ modelDiscounts: value.modelDiscounts.filter((_, i) => i !== index) })
                    }
                  >
                    {c('移除')}
                  </Button>
                </li>
              );
            })}
          </ul>
        ) : (
          <p className="team-discount-empty">{c('暂未设置折扣，所有模型按原价计费。')}</p>
        )}
      </div>
    </>
  );
}
