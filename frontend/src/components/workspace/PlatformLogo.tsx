import type { PlatformSettings } from '../../api/platform-client';
import { usePlatform } from '../../state/PlatformContext';
import { useCatalogText } from '../../contexts/useCatalogText';

export function PlatformLogo({ className = '', settings: supplied }: {
  className?: string;
  settings?: Pick<PlatformSettings, 'logoUrl'> | null;
}) {
  const { c } = useCatalogText();
  const platform = usePlatform();
  const settings = supplied === undefined ? platform.settings : supplied;
  return (
    <span className={'workspace-brand-mark ' + className} data-custom={!!settings?.logoUrl} aria-hidden>
      {settings?.logoUrl ? <img src={settings.logoUrl} alt="" /> : settings ? c('剧') : null}
    </span>
  );
}
