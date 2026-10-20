import { useCatalogText } from '../../contexts/useCatalogText';
import { Link, useLocation } from 'react-router-dom';
import { PanelLeft, LayoutGrid, BookOpen, FolderClosed, Clapperboard, Images } from 'lucide-react';
import { useLanguage } from '../../contexts/LanguageContext';
import { PlatformLogo } from './PlatformLogo';
import { SidebarLogoNav } from './SidebarLogoNav';
import { useWorkspace } from '../../state/WorkspaceContext';
import { ControlTooltip } from '../ui/control-tooltip';
import { Button } from '../ui/button';
import { WorkspaceSidebarFooter } from './WorkspaceSidebarFooter';
export function WorkspaceSidebar({
  collapsed,
  onToggleCollapsed,
}: {
  collapsed: boolean;
  onToggleCollapsed: () => void;
}) {
  const { c } = useCatalogText();
  const { t } = useLanguage(),
    { status } = useWorkspace();
  const { pathname } = useLocation();
  const projectId = pathname.startsWith('/playlet/') ? pathname.split('/')[2] : null;
  if (projectId)
    return (
      <>
        <aside className="jm-sidebar xy-project-rail" aria-label={c('项目导航')}>
          <ControlTooltip label={c('返回项目列表')}>
            <Link className="xy-rail-brand" to="/projects" aria-label={c('返回项目列表')}>
              <PlatformLogo />
            </Link>
          </ControlTooltip>
          <nav aria-label={c('项目页面')} className="xy-rail-nav">
            <ControlTooltip label={c('剧本内容')}>
              <Link
                to={`/playlet/${projectId}/outline`}
                aria-label={c('剧本内容')}
                aria-current={pathname.endsWith('/outline') ? 'page' : undefined}
                className="xy-rail-link"
              >
                <BookOpen aria-hidden />
              </Link>
            </ControlTooltip>
            <ControlTooltip label={c('资产库')}>
              <Link
                to={`/playlet/${projectId}/characters`}
                aria-label={c('资产库')}
                aria-current={
                  /^\/playlet\/[^/]+\/(characters|scenes|props)(\/|$)/.test(pathname)
                    ? 'page'
                    : undefined
                }
                className="xy-rail-link"
              >
                <FolderClosed aria-hidden />
              </Link>
            </ControlTooltip>
            <ControlTooltip label={c('分集视频')}>
              <Link
                to={`/playlet/${projectId}/episodes`}
                aria-label={c('分集视频')}
                aria-current={/\/(episodes|production)$/.test(pathname) ? 'page' : undefined}
                className="xy-rail-link"
              >
                <Clapperboard aria-hidden />
              </Link>
            </ControlTooltip>
          </nav>
          <WorkspaceSidebarFooter compact />
        </aside>
      </>
    );
  const toggleLabel = collapsed ? t('workspace.sidebar.expand') : t('workspace.sidebar.collapse');
  const navLabel = t('workspace.nav.playletAgent');
  const projectLink = (
    <Link
      to="/projects"
      aria-current={pathname === '/projects' ? 'page' : undefined}
      aria-label={navLabel}
      className="jm-sidebar-nav"
    >
      <LayoutGrid aria-hidden />
      <span className={collapsed ? 'sr-only' : undefined}>{navLabel}</span>
    </Link>
  );
  return (
    <aside className="jm-sidebar" data-collapsed={collapsed}>
      <header className="jm-sidebar-head">
        {!collapsed ? <SidebarLogoNav /> : null}
        <ControlTooltip label={toggleLabel}>
          <Button
            variant="ghost"
            className="jm-button--collapse"
            onClick={onToggleCollapsed}
            aria-label={toggleLabel}
            aria-expanded={!collapsed}
            aria-controls="workspace-sidebar-navigation"
          >
            {collapsed ? <PlatformLogo className="jm-sidebar-collapsed-mark" /> : null}
            <PanelLeft className={collapsed ? 'jm-sidebar-expand-icon' : undefined} />
          </Button>
        </ControlTooltip>
      </header>
      <nav id="workspace-sidebar-navigation" className="jm-sidebar-links" aria-label="Main">
        {collapsed ? <ControlTooltip label={navLabel}>{projectLink}</ControlTooltip> : projectLink}
        <ControlTooltip label={c('创作资产')}>
          <Link
            to="/creations"
            className="jm-sidebar-nav"
            aria-label={c('创作资产')}
            aria-current={pathname === '/creations' ? 'page' : undefined}
          >
            <Images aria-hidden />
            <span className={collapsed ? 'sr-only' : undefined}>{c('创作资产')}</span>
          </Link>
        </ControlTooltip>
      </nav>
      <WorkspaceSidebarFooter compact={collapsed} />
      {status?.readOnly ? <p className="workspace-service-note">{c('只读浏览')}</p> : null}
    </aside>
  );
}
