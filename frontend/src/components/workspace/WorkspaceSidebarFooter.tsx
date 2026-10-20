import { NotificationCenter } from '../notifications/NotificationCenter';
import { WorkspacePreferencesMenu } from './WorkspacePreferencesMenu';

export function WorkspaceSidebarFooter({ compact }: { compact: boolean }) {
  return (
    <footer className="xy-sidebar-footer" data-compact={compact}>
      <NotificationCenter compact={compact} />
      <div className="xy-sidebar-footer-separator" role="separator" />
      <WorkspacePreferencesMenu compact={compact} />
    </footer>
  );
}
