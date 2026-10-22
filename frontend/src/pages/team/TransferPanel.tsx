import { useState, type FormEvent } from 'react';
import { teamClient } from '../../api/team-client';
import type { TeamMember, TransferReceipt } from '../../api/spaces-types';
import { Button } from '../../components/ui/button';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Choice, Field } from '../operations/controls';
import { ConfirmAction, MutationFeedback, useTeamMutation } from './team-ui';

export function TransferPanel({
  id,
  members,
  onSaved,
  disabled = false,
}: {
  id: string;
  members: TeamMember[];
  onSaved: () => void;
  disabled?: boolean;
}) {
  const { c } = useCatalogText(),
    mutation = useTeamMutation();
  const [from, setFrom] = useState(''),
    [to, setTo] = useState(''),
    [receipts, setReceipts] = useState<TransferReceipt[] | null>(null),
    [confirm, setConfirm] = useState(false),
    [error, setError] = useState('');
  const locked = disabled || mutation.busy || mutation.needsRead;
  const recipient = members.find((member) => member.userId === to);
  function submit(event: FormEvent) {
    event.preventDefault();
    if (locked || !from.trim() || !recipient) return;
    setError('');
    if (members.some((member) => member.userId === from.trim())) {
      setError(c('只能移交已经离队的成员资源。'));
      return;
    }
    setConfirm(true);
  }
  function resourceName(kind: string) {
    return kind === 'project'
      ? c('短剧项目')
      : kind === 'upload'
        ? c('上传文件')
        : kind === 'media'
          ? c('创作资产')
          : kind;
  }
  return (
    <section className="team-resource-transfer" aria-label={c('移交离队成员资源')}>
      <p className="op-muted team-transfer-description">
        {c('保留原创建人。每项分别处理，可能部分成功，请逐项核对结果。')}
      </p>
      <form className="team-form" onSubmit={submit}>
        <Field label={c('离队成员用户 ID')}>
          <input
            required
            value={from}
            disabled={locked}
            onChange={(event) => setFrom(event.target.value)}
          />
        </Field>
        <Field label={c('接收成员')}>
          <Choice
            label={c('请选择接收成员')}
            value={to}
            options={members.map((member) => ({ value: member.userId, label: member.name }))}
            onChange={setTo}
            disabled={locked}
          />
        </Field>
        {error && (
          <p className="op-inline-error" role="alert">
            {error}
          </p>
        )}
        <MutationFeedback mutation={mutation} />
        <div className="team-transfer-actions">
          <Button type="submit" disabled={locked || !from.trim() || !recipient}>
            {c('核对并移交')}
          </Button>
          {mutation.needsRead && (
            <Button
              variant="outline"
              disabled={mutation.busy || disabled}
              onClick={() => void mutation.readback(() => teamClient.members(id), onSaved)}
            >
              {c('读取最新状态')}
            </Button>
          )}
        </div>
      </form>
      {receipts && (
        <div className="team-transfer-receipts" role="status">
          <h4>{c('逐项移交结果')}</h4>
          {receipts.length === 0 && <p>{c('没有找到可移交的资源。')}</p>}
          {receipts.map((receipt, index) => (
            <p className="team-transfer-result" key={receipt.kind + receipt.id + index}>
              <span>
                {resourceName(receipt.kind)}
                <small className="team-identifier">{receipt.id}</small>
              </span>
              <span className={receipt.transferred ? 'team-transfer-done' : 'op-inline-error'}>
                {receipt.transferred
                  ? c('已移交')
                  : c('未移交：{code}', { code: receipt.errorCode || 'UNKNOWN' })}
              </span>
            </p>
          ))}
        </div>
      )}
      <ConfirmAction
        open={confirm}
        onOpenChange={setConfirm}
        busy={mutation.busy}
        confirmDisabled={locked || !recipient}
        title={c('确认资源移交')}
        description={c('将全部资源移交给 {name}。可能部分成功，请逐项核对结果。', {
          name: recipient?.name || '',
        })}
        confirm={() => {
          if (locked || !recipient) return;
          void mutation.run(
            () =>
              teamClient.transfer(id, {
                fromUserId: from.trim(),
                toUserId: to,
                all: true,
                resources: [],
              }),
            (result) => {
              setReceipts(result);
              setConfirm(false);
              onSaved();
            },
          );
        }}
      >
        <MutationFeedback mutation={mutation} />
      </ConfirmAction>
    </section>
  );
}
