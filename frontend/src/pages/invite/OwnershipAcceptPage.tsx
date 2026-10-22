import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { teamClient } from '../../api/team-client';
import { useSpace } from '../../state/SpaceContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Button } from '../../components/ui/button';
import { MutationFeedback, useTeamMutation } from '../team/team-ui';
export function OwnershipAcceptPage() {
  const { spaceId = '', transferId = '' } = useParams(),
    space = useSpace(),
    { c } = useCatalogText(),
    mutation = useTeamMutation(),
    [accepted, setAccepted] = useState(false);
  const name = space.spaces.find((item) => item.id === spaceId)?.name;
  return (
    <section className="op-page team-page team-confirm-page">
      <div className="team-card">
        <h1>{c('确认接任团队所有者')}</h1>
        {name && <h2>{name}</h2>}
        <p>
          {c(
            '确认后你将成为团队所有者，原所有者变为管理员。只有最新提议可以确认；已被替换或成员身份失效的提议无法生效。',
          )}
        </p>
        <MutationFeedback mutation={mutation} />
        {accepted ? (
          <p role="status">{c('所有权转让已确认。')}</p>
        ) : (
          <Button
            busy={mutation.busy}
            disabled={mutation.needsRead}
            onClick={() =>
              void mutation.run(
                () => teamClient.acceptOwnership(spaceId, transferId),
                async () => {
                  setAccepted(true);
                  await space.refresh();
                },
              )
            }
          >
            {c('确认接任')}
          </Button>
        )}
        <Button asChild variant="outline">
          <Link to={'/team/' + encodeURIComponent(spaceId)}>{c('查看团队')}</Link>
        </Button>
      </div>
    </section>
  );
}
