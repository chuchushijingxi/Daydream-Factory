import { useState, type FormEvent } from 'react';
import { teamClient } from '../../api/team-client';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '../../components/ui/dialog';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Field } from '../operations/controls';
import { MutationFeedback, useTeamMutation } from './team-ui';

function JoinForm() {
  const { c } = useCatalogText(),
    mutation = useTeamMutation();
  const [id, setId] = useState(''),
    [message, setMessage] = useState(''),
    [sent, setSent] = useState('');
  // 一次提交完成核对与申请：先按 ID 读出团队名称，再发送申请，成功后回显名称供用户确认。
  async function submit(e: FormEvent) {
    e.preventDefault();
    const target = id.trim();
    await mutation.run(
      async () => {
        let name: string;
        try {
          ({ name } = await teamClient.lookup(target));
        } catch (error) {
          if ((error as { code?: string }).code === 'NOT_FOUND')
            throw new Error(c('未找到该团队，请核对团队 ID。'));
          throw error;
        }
        await teamClient.join(target, message.trim());
        return name;
      },
      (name) => setSent(name),
    );
  }
  return (
    <>
      <DialogTitle>{c('加入团队')}</DialogTitle>
      <DialogDescription>
        {c('输入团队 ID 发送申请，团队所有者或管理员审批后即可加入。')}
      </DialogDescription>
      {sent ? (
        <p className="team-success" role="status">
          {c('已向「{name}」发送加入申请，请等待团队所有者或管理员处理。', { name: sent })}
        </p>
      ) : (
        <>
          <MutationFeedback mutation={mutation} />
          <form className="team-form" onSubmit={submit}>
            <Field label={c('团队 ID')}>
              <input
                required
                value={id}
                onChange={(e) => {
                  setId(e.target.value);
                  mutation.reconciled();
                }}
              />
            </Field>
            <Field label={c('申请说明（选填）')} hint={c('最多 200 个字符')}>
              <textarea
                maxLength={200}
                value={message}
                onChange={(e) => setMessage(e.target.value)}
              />
            </Field>
            <Button type="submit" busy={mutation.busy} disabled={!id.trim() || mutation.needsRead}>
              {c('发送加入申请')}
            </Button>
          </form>
        </>
      )}
    </>
  );
}
export function JoinTeamDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="team-dialog team-join-dialog">
        {open && <JoinForm />}
      </DialogContent>
    </Dialog>
  );
}
