import { CreditShortageNotice } from '../../features/credits/CreditShortageNotice';
import { useState } from 'react';
import { ResourceSharingDialog } from '../spaces/ResourceSharingDialog';
import { useWorkspace } from '../../state/WorkspaceContext';
import { ProjectSelectionSummary } from './ProjectSelectionSummary';
import { CreditBalanceMenu } from './CreditBalanceMenu';
import { Link, useLocation, useSearchParams } from 'react-router-dom';
import { useCatalogText } from '../../contexts/useCatalogText';
export function MainWorkspaceTopToolbar() {
  const { c } = useCatalogText();
  const {
    currentProject,
    storyboardIndex,
    canShare,
    projectReadOnlyReason,
    refreshProjectPermissions,
  } = useWorkspace();
  const [sharing, setSharing] = useState(false);
  const { pathname } = useLocation(),
    [params] = useSearchParams();
  const production = pathname.endsWith('/production');
  const episode = storyboardIndex?.index?.episodes.find(
    (item) => item.id === params.get('episodeId'),
  );
  if (!currentProject)
    return (
      <header className="xy-home-titlebar">
        <CreditBalanceMenu />
      </header>
    );
  return (
    <div
      className="xy-project-titlebar jm-workspace-surface flex min-h-[56px] w-full shrink-0 items-center"
      data-production={production}
    >
      <div className="flex min-h-0 min-w-0 flex-1 items-center overflow-hidden px-4 py-2 pl-6 sm:pl-8">
        <div className="w-full max-w-[1480px] min-w-0">
          {production ? (
            <div className="xy-production-breadcrumb">
              <Link to={`/playlet/${currentProject.id}/episodes`} aria-label={c('返回分集视频')}>
                {c('分集视频')}
              </Link>
              <span className="xy-production-divider" aria-hidden="true">
                /
              </span>
              <span
                className="xy-production-episode"
                title={episode?.title || currentProject.title}
              >
                {episode?.title || currentProject.title}
              </span>
            </div>
          ) : (
            <h1 className="truncate text-[14px] font-medium" title={currentProject.title}>
              {currentProject.title}
            </h1>
          )}
        </div>
      </div>
      {projectReadOnlyReason ? (
        <div className="project-editing-banner" role="status">
          <span>{projectReadOnlyReason}</span>
          <button type="button" onClick={() => void refreshProjectPermissions()}>
            {c('重新核对')}
          </button>
        </div>
      ) : null}
      {canShare ? (
        <button type="button" className="jm-button" onClick={() => setSharing(true)}>
          {c('共享')}
        </button>
      ) : null}
      <ResourceSharingDialog
        open={sharing}
        onOpenChange={setSharing}
        spaceId={currentProject.spaceId}
        projectId={currentProject.id}
      />
      <CreditShortageNotice
        code={currentProject.runs.at(-1)?.errorCode}
        needed={currentProject.runs.at(-1)?.credit?.amount}
      />
      <ProjectSelectionSummary />
      <CreditBalanceMenu />
    </div>
  );
}
