import { useCatalogText } from './contexts/useCatalogText';
import { useState } from 'react';
import {
  Navigate,
  Route,
  Routes,
  useLocation,
  useParams,
  useSearchParams,
} from 'react-router-dom';
import { Button } from './components/ui/button';
import { Spinner } from './components/ui/spinner';
import { useWorkspace } from './state/WorkspaceContext';
import { WorkspaceSidebar } from './components/workspace/WorkspaceSidebar';
import { MainWorkspaceTopToolbar } from './components/workspace/MainWorkspaceTopToolbar';
import { ProjectListPage } from './pages/projects/ProjectListPage';
import { ScriptContentPage } from './pages/script/ScriptContentPage';
import { AssetCatalogPage } from './pages/assets/AssetCatalogPage';
import { CharacterLooksPage } from './pages/assets/CharacterLooksPage';
import { ProductionPage } from './pages/production/ProductionPage';
import { EpisodesPage } from './pages/production/EpisodesPage';
import { indexReady } from './domain/storyboard';
import { CreationsPage } from './pages/creations/CreationsPage';
import { CreditsPage } from './pages/credits/CreditsPage';
import { PersonalCreditsProvider } from './features/credits/PersonalCredits';
import { DailyGiftClaim } from './features/credits/DailyGiftClaim';
import { TeamSettingsPage } from './pages/team/TeamSettingsPage';
import { PendingInvitesPage } from './pages/invite/PendingInvitesPage';
import { InviteAcceptPage } from './pages/invite/InviteAcceptPage';
import { OwnershipAcceptPage } from './pages/invite/OwnershipAcceptPage';
import { RechargeProvider } from './features/payments/Recharge';
import { RechargeOrdersPage } from './pages/payments/RechargeOrdersPage';
function AssetStep({ children }: { children: React.ReactNode }) {
  const { c } = useCatalogText();
  const { currentProject: p, status, storyboardIndex, storyboardError } = useWorkspace();
  if (p && status?.capabilities.storyboard && !storyboardIndex && !storyboardError)
    return (
      <div className="p-6 flex items-center gap-2" role="status">
        <Spinner />
        {c('读取分集解析状态…')}
      </div>
    );
  if (
    p &&
    status?.capabilities.storyboard &&
    (storyboardError || storyboardIndex?.index || storyboardIndex?.latestIndexRun) &&
    !indexReady(storyboardIndex, p.source.version)
  )
    return <Navigate to={`/playlet/${p.id}/outline`} replace />;
  return children;
}
function ProductionRoute() {
  const { id } = useParams(),
    [params] = useSearchParams();
  return params.get('episodeId') ? (
    <ProductionPage />
  ) : (
    <Navigate to={`/playlet/${id}/episodes`} replace />
  );
}
function WorkspaceContent() {
  const { c } = useCatalogText();
  const { currentProject, error, busy } = useWorkspace();
  return (
    <>
      <MainWorkspaceTopToolbar />
      <div className="xy-workspace-content flex min-h-0 flex-1 flex-col overflow-hidden">
        {error ? (
          <div
            role="alert"
            className="m-6 rounded-lg border border-red-300 p-4 text-red-600 dark:text-red-400"
          >
            {error}
            <Button variant="link" size="sm" className="ml-4" onClick={() => location.reload()}>
              {c('重新读取')}
            </Button>
          </div>
        ) : null}
        {error && !currentProject ? null : busy && !currentProject ? (
          <div
            className="flex items-center gap-2 p-6 text-slate-500"
            role="status"
            aria-busy="true"
          >
            <Spinner />
            {c('读取真实项目…')}
          </div>
        ) : (
          <Routes>
            <Route path="/team/:spaceId" element={<TeamSettingsPage />}>
              <Route path="contract" element={null} />
            </Route>
            <Route path="/invites" element={<PendingInvitesPage />} />
            <Route path="/invite/:token" element={<InviteAcceptPage />} />
            <Route path="/team/:spaceId/ownership/:transferId" element={<OwnershipAcceptPage />} />
            <Route path="/projects" element={<ProjectListPage />} />
            <Route path="/credits" element={<CreditsPage />} />
            <Route path="/payments/orders" element={<RechargeOrdersPage />} />
            <Route path="/creations" element={<CreationsPage />} />
            <Route path="/playlet/:id/outline" element={<ScriptContentPage />} />
            <Route
              path="/playlet/:id/characters"
              element={
                <AssetStep>
                  <AssetCatalogPage kind="character" />
                </AssetStep>
              }
            />
            <Route
              path="/playlet/:id/scenes"
              element={
                <AssetStep>
                  <AssetCatalogPage kind="location" />
                </AssetStep>
              }
            />
            <Route
              path="/playlet/:id/props"
              element={
                <AssetStep>
                  <AssetCatalogPage kind="prop" />
                </AssetStep>
              }
            />
            <Route path="/playlet/:id/episodes" element={<EpisodesPage />} />
            <Route path="/playlet/:id/production" element={<ProductionRoute />} />
            <Route
              path="/playlet/:id/characters/:characterId/canvas"
              element={
                <AssetStep>
                  <CharacterLooksPage />
                </AssetStep>
              }
            />
            <Route path="*" element={<Navigate to="/projects" replace />} />
          </Routes>
        )}
      </div>
    </>
  );
}
// 工作台仅组装页面与侧栏，业务保存留在页面用例和 API 客户端。
function WorkspaceShell() {
  const [sidebarCollapsed, setSidebarCollapsed] = useState(true);
  const { pathname } = useLocation();
  return (
    <div
      className={
        'jm-workspace jm-workspace-surface flex h-screen w-full overflow-hidden transition-colors' +
        (pathname.startsWith('/playlet/')
          ? ' xy-project-workspace'
          : pathname === '/credits'
            ? ' personal-credits-workspace'
            : pathname === '/projects'
              ? ' xy-home-workspace'
              : pathname === '/creations'
                ? ' creations-workspace'
                : pathname === '/invites' ||
                    pathname.startsWith('/invite/') ||
                    pathname.startsWith('/team/')
                  ? ' space-settings-workspace'
                  : '') +
        (/^\/playlet\/[^/]+\/(outline|episodes|production)$/.test(pathname)
          ? ' editor-workspace'
          : '')
      }
    >
      <WorkspaceSidebar
        collapsed={sidebarCollapsed}
        onToggleCollapsed={() => setSidebarCollapsed((c) => !c)}
      />
      <div className="xy-project-main flex min-w-0 flex-1 flex-col overflow-hidden">
        <DailyGiftClaim />
        <WorkspaceContent />
      </div>
    </div>
  );
}
export function MainWorkspace() {
  return (
    <PersonalCreditsProvider>
      <RechargeProvider>
        <WorkspaceShell />
      </RechargeProvider>
    </PersonalCreditsProvider>
  );
}
