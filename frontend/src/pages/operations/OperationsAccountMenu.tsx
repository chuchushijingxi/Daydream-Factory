import { useSession } from '../../state/SessionContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { SidebarAccountMenu } from '../../components/workspace/SidebarAccountMenu';

export function OperationsAccountMenu({ compact }: { compact: boolean }) {
  const { user } = useSession();
  const { c } = useCatalogText();
  return user ? (
    <SidebarAccountMenu
      compact={compact}
      label={c('账号菜单：{name}', { name: user.username })}
      showLanguage
    />
  ) : null;
}
