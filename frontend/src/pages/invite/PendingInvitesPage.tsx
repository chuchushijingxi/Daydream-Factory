import { useCatalogText } from '../../contexts/useCatalogText';
import { useSpace } from '../../state/SpaceContext';
import { teamClient } from '../../api/team-client';
import { Button } from '../../components/ui/button';
import { Feedback, useRead } from '../operations/controls';
import { dateTime } from '../credits/credit-format';
import { MutationFeedback, useTeamMutation } from '../team/team-ui';
export function PendingInvitesPage() {
  const { c } = useCatalogText(),
    space = useSpace(),
    mutation = useTeamMutation();
  const read = useRead((signal) => teamClient.pending(signal), [space.epoch]);
  const refresh = () => {
    void mutation.readback(
      () => teamClient.pending(),
      async () => {
        read.refresh();
        await space.refresh();
      },
    );
  };
  return (
    <section className="op-page team-page">
      <header className="op-page-heading">
        <div>
          <h1>{c('待处理邀请')}</h1>
          <p>{c('加入团队前请核对团队名称，邀请有效期为 7 天。')}</p>
        </div>
        {mutation.needsRead && (
          <Button variant="outline" onClick={refresh} disabled={mutation.busy}>
            {c('读取最新状态')}
          </Button>
        )}
      </header>
      <MutationFeedback mutation={mutation} />
      <Feedback
        busy={read.busy}
        error={read.error}
        empty={!read.value?.length}
        retry={refresh}
      />
      {read.value?.map((invite) => (
        <article className="team-card team-invite-row" key={invite.id}>
          <div>
            <h2>{invite.teamName}</h2>
            <p>{c('有效期至 {time}', { time: dateTime(invite.expiresAt) })}</p>
          </div>
          <div className="op-row-actions">
            <Button
              variant="outline"
              disabled={mutation.busy || mutation.needsRead}
              onClick={() =>
                void mutation.run(
                  () => teamClient.declineInvite(invite.id),
                  async () => {
                    read.refresh();
                    await space.refresh();
                  },
                )
              }
            >
              {c('拒绝')}
            </Button>
            <Button
              disabled={mutation.busy || mutation.needsRead}
              onClick={() =>
                void mutation.run(
                  () => teamClient.acceptInvite(invite.id),
                  async (value) => {
                    await space.refresh();
                    await space.switchSpace(value.id);
                  },
                )
              }
            >
              {c('接受并加入')}
            </Button>
          </div>
        </article>
      ))}
    </section>
  );
}
