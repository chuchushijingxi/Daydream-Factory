import { useCatalogText } from '../../contexts/useCatalogText';
import { SidebarAccountMenu } from './SidebarAccountMenu';

export function WorkspacePreferencesMenu({ compact = false }: { compact?: boolean }) {
  const { c } = useCatalogText();
  return <SidebarAccountMenu compact={compact} label={c('工作台菜单')} showLanguage />;
}
