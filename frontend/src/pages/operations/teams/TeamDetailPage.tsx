import { useState, type FormEvent } from 'react';
import { Link, useParams } from 'react-router-dom';
import { teamClient } from '../../../api/team-client';
import type { TeamView } from '../../../api/spaces-types';
import { Button } from '../../../components/ui/button';
import { useCatalogText } from '../../../contexts/useCatalogText';
import { Feedback, Field, useRead } from '../controls';
import { dateTime, formatInteger } from '../../credits/credit-format';
import { ArrowLeft } from 'lucide-react';
import { ModelSwitch } from '../../model-config/ModelConfigurationControls';
import {
  ConfirmAction,
  MutationFeedback,
  TeamState,
  useTeamMutation,
} from '../../team/team-ui';
import {
  TeamContractFields,
  shanghaiDraft,
  buildTeamPatch,
  discountsComplete,
  type ContractDraft,
} from './TeamContractFields';
function ContractEditor({ original }: { original: TeamView }) {
  const { c } = useCatalogText(),
    mutation = useTeamMutation();
  const [saved, setSaved] = useState(original),
    // 留空表示不更换所有者；只有填写其他成员 ID 才会提交更换。
    [ownerId, setOwnerId] = useState(''),
    [disabled, setDisabled] = useState(original.disabled),
    [confirm, setConfirm] = useState(false);
  const [draft, setDraft] = useState<ContractDraft>({
    memberLimit: String(original.memberLimit),
    grantAmount: String(original.grantAmount),
    expiresAt: shanghaiDraft(original.expiresAt),
    modelDiscounts: original.modelDiscounts,
  });
  const models = useRead((signal) => teamClient.models(signal), []);
  const patch = (value: Partial<ContractDraft>) => setDraft((old) => ({ ...old, ...value }));
  const nextOwner = ownerId.trim() || saved.ownerId;
  const changed =
    nextOwner !== saved.ownerId ||
    disabled !== saved.disabled ||
    draft.memberLimit !== String(saved.memberLimit) ||
    draft.grantAmount !== String(saved.grantAmount) ||
    draft.expiresAt !== shanghaiDraft(saved.expiresAt) ||
    JSON.stringify(draft.modelDiscounts) !== JSON.stringify(saved.modelDiscounts);
  const reset = (value: TeamView) => {
    setDraft({
      memberLimit: String(value.memberLimit),
      grantAmount: String(value.grantAmount),
      expiresAt: shanghaiDraft(value.expiresAt),
      modelDiscounts: value.modelDiscounts,
    });
    setOwnerId('');
    setDisabled(value.disabled);
  };
  async function save() {
    await mutation.run(
      () => teamClient.patchTeam(saved.id, buildTeamPatch(saved, draft, nextOwner, disabled)),
      (value) => {
        setSaved(value);
        setConfirm(false);
        reset(value);
      },
    );
  }
  const reread = () => mutation.readback(() => teamClient.operationsTeam(saved.id), setSaved);
  function submit(e: FormEvent) {
    e.preventDefault();
    setConfirm(true);
  }
  const locked = mutation.busy || mutation.needsRead;
  return (
    <div className="team-contract-page">
      <div className="team-contract-status">
        <TeamState team={saved} />
        {mutation.needsRead && (
          <Button variant="outline" size="sm" disabled={mutation.busy} onClick={() => void reread()}>
            {c('读取最新状态')}
          </Button>
        )}
      </div>
      <dl className="team-contract-overview">
        <div>
          <dt>{c('当前所有者')}</dt>
          <dd>
            {saved.ownerName}
            {saved.ownerEmail ? <small>{saved.ownerEmail}</small> : null}
          </dd>
        </div>
        <div>
          <dt>{c('成员人数')}</dt>
          <dd>{c('{count} / {limit} 人', { count: saved.memberCount, limit: saved.memberLimit })}</dd>
        </div>
        <div>
          <dt>{c('团队总余额')}</dt>
          <dd>{formatInteger(String(saved.balance))}</dd>
        </div>
        <div>
          <dt>{c('每 30 天发放积分')}</dt>
          <dd>{formatInteger(String(saved.grantAmount))}</dd>
        </div>
        <div>
          <dt>{c('开通时间')}</dt>
          <dd>{dateTime(saved.openedAt)}</dd>
        </div>
        <div>
          <dt>{c('到期时间')}</dt>
          <dd>{dateTime(saved.expiresAt)}</dd>
        </div>
        <div>
          <dt>{c('下次自动发放')}</dt>
          <dd>{dateTime(saved.nextGrantAt)}</dd>
        </div>
        <div>
          <dt>{c('模型折扣')}</dt>
          <dd>
            {saved.modelDiscounts.length
              ? c('{count} 个模型', { count: saved.modelDiscounts.length })
              : c('未设置')}
          </dd>
        </div>
      </dl>
      <form className="team-contract-form" onSubmit={submit}>
        <section>
          <header>
            <h2>{c('合同条款')}</h2>
            <p className="op-muted">
              {c('发放与续约节奏由服务端计算。停用期间不发放，启用跳过停用期，不补发。')}
            </p>
          </header>
          <Feedback busy={models.busy} error={models.error} retry={models.refresh} />
          <TeamContractFields
            value={draft}
            patch={patch}
            models={models.value || []}
            disabled={locked}
          />
        </section>
        <section>
          <header>
            <h2>{c('所有者与状态')}</h2>
          </header>
          <div className="team-contract-grid">
            <Field
              label={c('更换所有者（可选）')}
              hint={c('填写该团队当前成员的用户 ID；留空则不更换，不会代为添加成员。')}
            >
              <input
                value={ownerId}
                placeholder={c('当前：{name}', { name: saved.ownerName })}
                onChange={(e) => setOwnerId(e.target.value)}
                disabled={locked}
              />
            </Field>
          </div>
          <div className="team-contract-switch">
            <div>
              <strong>{c('停用团队')}</strong>
              <p className="op-muted">{c('停用后全体成员无法进入，积分冻结；重新启用后恢复。')}</p>
            </div>
            <ModelSwitch
              id={'team-disabled-' + saved.id}
              label={c('停用团队')}
              checked={disabled}
              disabled={locked}
              onChange={() => setDisabled((value) => !value)}
            />
          </div>
        </section>
        <MutationFeedback mutation={mutation} />
        <footer className="team-contract-actions">
          <Button
            type="button"
            variant="outline"
            disabled={!changed || locked}
            onClick={() => reset(saved)}
          >
            {c('撤销修改')}
          </Button>
          <Button
            type="submit"
            busy={mutation.busy}
            disabled={
              !changed || mutation.needsRead || models.busy || !!models.error || !discountsComplete(draft)
            }
          >
            {c('保存合同设置')}
          </Button>
        </footer>
      </form>
      <ConfirmAction
        open={confirm}
        onOpenChange={setConfirm}
        busy={mutation.busy}
        title={c('确认修改团队合同')}
        description={c(
          '请核对到期时间、发放额、折扣、所有者和停启状态。修改立即生效，并写入团队操作记录。',
        )}
        confirm={() => void save()}
      >
        <MutationFeedback mutation={mutation} />
      </ConfirmAction>
    </div>
  );
}
export function TeamDetailPage() {
  const { id = '' } = useParams(),
    { c } = useCatalogText();
  const read = useRead((signal) => teamClient.operationsTeam(id, signal), [id]);
  return (
    <section className="op-page team-page">
      <Link className="op-back" to="/operations/teams">
        <ArrowLeft aria-hidden />
        {c('返回团队列表')}
      </Link>
      <header className="op-page-heading">
        <div>
          <h1>{read.value?.name || c('团队合同详情')}</h1>
          <p>{c('运营仅管理合同，不代成员查看内容、邀请或移出。')}</p>
        </div>
      </header>
      <Feedback busy={read.busy} error={read.error} retry={read.refresh} />
      {read.value && <ContractEditor key={id} original={read.value} />}
    </section>
  );
}
