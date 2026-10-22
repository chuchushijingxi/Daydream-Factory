import { useParams, Link } from 'react-router-dom';
import { teamClient } from '../../api/team-client';
import { useSpace } from '../../state/SpaceContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Button } from '../../components/ui/button';
import { Feedback, useRead } from '../operations/controls';
import { dateTime } from '../credits/credit-format';
import { MutationFeedback, useTeamMutation } from '../team/team-ui';
export function InviteAcceptPage() {
  const { token = '' } = useParams(),
    space = useSpace(),
    { c } = useCatalogText(),
    mutation = useTeamMutation();
  const read = useRead((signal) => teamClient.preview(token, signal), [token]);
  return (
    <section className="op-page team-page team-confirm-page">
      <div className="team-card">
        <h1>{c('确认加入团队')}</h1>
        <Feedback
          busy={read.busy}
          error={read.error}
          retry={() => {
            read.refresh();
            mutation.reconciled();
          }}
        />
        <MutationFeedback mutation={mutation} />
        {read.value && (
          <>
            <h2>{read.value.teamName}</h2>
            <p>{c('有效期至 {time}', { time: dateTime(read.value.expiresAt) })}</p>
            <p>{c('接受邀请后，你将成为该团队的成员。')}</p>
            <div className="op-row-actions">
              <Button variant="outline" asChild>
                <Link to="/projects">{c('暂不加入')}</Link>
              </Button>
              <Button
                busy={mutation.busy}
                disabled={
                  mutation.needsRead || new Date(read.value.expiresAt).getTime() <= Date.now()
                }
                onClick={() =>
                  void mutation.run(
                    () => teamClient.acceptLink(token),
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
          </>
        )}
      </div>
    </section>
  );
}
