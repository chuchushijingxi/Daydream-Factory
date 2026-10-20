import { usePlatform } from '../../state/PlatformContext';
import { PlatformLogo } from './PlatformLogo';
export function SidebarLogoNav() {
  const { settings } = usePlatform();
  return (
    <div className="workspace-brand" aria-label={settings?.name}>
      <PlatformLogo />
      <div>
        <strong title={settings?.name}>{settings?.name}</strong>
        {settings?.sidebarDescription ? <small title={settings.sidebarDescription}>{settings.sidebarDescription}</small> : null}
      </div>
    </div>
  );
}
